#!/usr/bin/env python3
#
# WHAT: builds the union catalog (ADR-0012 §1) from the OpenAPI spec committed at
#       every Langfuse release tag from v3.0.0 onward, and writes it to
#       internal/catalog/spec/langfuse-union-catalog.json, which the server embeds.
#       Each operation (method + path) keeps its definition from the last spec
#       that contained it, and carries its version range (x-introduced, plus
#       x-removed when it left the spec) and its operation family (x-family:
#       legacy, v4 read or experiments) where a write mode gates it.
# WHY:  ADR-0012 — a deployment gets every operation its version serves, so the
#       catalog must know operations older releases had and newer ones dropped.
#       The file is committed so that builds and pull-request CI stay offline.
# WHEN: weekly, by .github/workflows/union-catalog.yml, which opens a pull
#       request when the output changed; by hand when a Langfuse release matters
#       now. A changed operation set also changes the deployment-profile
#       fixtures of internal/catalog/resolve_test.go: update them in the same PR.
# HOW:  python3 scripts/gen-union-catalog.py [--repo DIR] [--out FILE]
#       --repo  blobless bare clone of langfuse/langfuse, created or refreshed
#               here (default: $XDG_CACHE_HOME/langfuse-api-mcp/langfuse.git)
#       --out   default internal/catalog/spec/langfuse-union-catalog.json
#
# Needs git, network access to github.com and PyYAML (pip install pyyaml).
# The first run downloads the commit and tree history (a few hundred MiB) and
# about 150 distinct spec blobs; later runs fetch only new tags.
#
# Approach proven on branch prototype/langfuse-api-versions (opmatrix.py).

import argparse
import json
import os
import re
import subprocess
import sys

import yaml

UPSTREAM = "https://github.com/langfuse/langfuse.git"
# The spec moved once; old tags keep it under generated/openapi-server/.
SPEC_PATHS = ["web/public/generated/api/openapi.yml", "generated/openapi-server/openapi.yml"]
FLOOR = (3, 0, 0)  # ADR-0012 §4: the supported floor
METHODS = ("get", "post", "put", "patch", "delete")

# Where the docs state an earlier floor than the spec, the earlier floor wins
# (ADR-0012 §1; docs/research/langfuse-api-versions.md §3). Keys are a
# "METHOD path" or a path prefix ending in "/" or "*" matched on the path alone.
DOCUMENTED_FLOORS = {
    "POST /api/public/otel/v1/traces": "3.22.0",
    "/api/public/v3/scores*": "3.179.0",
}

# Operation families gated by the write mode (ADR-0012 §2). Path prefixes of the
# v4 read and experiments families; the legacy family is every operation the
# newest spec still lists and marks deprecated (the v4.12 deprecation of the
# legacy reads, dataset runs and run items). An operation deprecated and then
# removed (unstable/evaluators, deprecated in v4.23, gone in v4.31) is bounded
# by its range, not gated by a family.
FAMILY_PREFIXES = [
    ("/api/public/v2/observations", "v4 read"),
    ("/api/public/v2/metrics", "v4 read"),
    ("/api/public/experiments", "experiments"),
    ("/api/public/experiment-items", "experiments"),
]


def version(tag):
    return tuple(int(x) for x in tag[1:].split("."))


def text(v):
    return ".".join(str(x) for x in v)


def git(repo, *args, data=None):
    return subprocess.run(["git", "-C", repo, *args], input=data, capture_output=True, check=True).stdout


def refresh(repo):
    if not os.path.isdir(repo):
        os.makedirs(os.path.dirname(repo), exist_ok=True)
        subprocess.run(["git", "clone", "--quiet", "--bare", "--filter=blob:none", UPSTREAM, repo], check=True)
    git(repo, "fetch", "--quiet", "--force", "--filter=blob:none", "origin", "+refs/tags/*:refs/tags/*")


def release_tags(repo):
    """Every plain vX.Y.Z tag from the floor on, oldest first, with its spec blob."""
    tags = [t for t in git(repo, "tag", "--list", "v*").decode().split() if re.fullmatch(r"v\d+\.\d+\.\d+", t)]
    tags = sorted((t for t in tags if version(t) >= FLOOR), key=version)
    blobs = {}
    for t in tags:
        # ls-tree reads trees only: it names the blob without fetching it.
        out = git(repo, "ls-tree", "-r", t, "--", *SPEC_PATHS).decode().splitlines()
        found = {line.split("\t", 1)[1]: line.split()[2] for line in out}
        for p in SPEC_PATHS:
            if p in found:
                blobs[t] = found[p]
                break
    missing = [t for t in tags if t not in blobs]
    if missing:
        sys.exit(f"no OpenAPI spec found at tags {missing}")
    # One fetch for every spec blob, instead of one lazy fetch per blob.
    distinct = sorted(set(blobs.values()))
    git(repo, "-c", "fetch.negotiationAlgorithm=noop", "fetch", "--quiet", "--no-tags", "--no-write-fetch-head",
        "--recurse-submodules=no", "--filter=blob:none", "--stdin", "origin", data="\n".join(distinct).encode())
    return [(t, blobs[t]) for t in tags]


def load_spec(repo, blob):
    src = git(repo, "cat-file", "blob", blob)
    return yaml.load(src, Loader=getattr(yaml, "CSafeLoader", yaml.SafeLoader))


def deref(node, spec, seen=()):
    """node with every $ref it holds inlined from spec's components."""
    if isinstance(node, list):
        return [deref(x, spec, seen) for x in node]
    if not isinstance(node, dict):
        return node
    ref = node.get("$ref")
    if isinstance(ref, str):
        if ref in seen or not ref.startswith("#/components/"):
            sys.exit(f"unresolvable reference {ref}")
        target = spec
        for part in ref[2:].split("/"):
            target = target[part]
        merged = deref(target, spec, seen + (ref,))
        rest = {k: deref(v, spec, seen) for k, v in node.items() if k != "$ref"}
        if merged.get("nullable") or rest.get("nullable"):
            rest["nullable"] = True
        return {**merged, **rest}
    return {k: deref(v, spec, seen) for k, v in node.items()}


def operations(spec):
    """{"METHOD path": operation} of one spec, parameters inlined."""
    out = {}
    for path, item in (spec.get("paths") or {}).items():
        shared = item.get("parameters") or []
        for method in METHODS:
            op = item.get(method)
            if not isinstance(op, dict):
                continue
            params = {}
            for p in deref(shared, spec) + deref(op.get("parameters") or [], spec):
                params[(p.get("name"), p.get("in"))] = p  # operation-level wins
            kept = [p for p in params.values() if p.get("in") in ("path", "query")]
            for p in kept:
                p.pop("description", None)
                if isinstance(p.get("schema"), dict):
                    p["schema"].pop("description", None)
            out[f"{method.upper()} {path}"] = {
                "operationId": op.get("operationId"),
                "tags": op.get("tags") or [],
                "description": op.get("description") or op.get("summary") or "",
                "deprecated": bool(op.get("deprecated")),
                "parameters": kept,
            }
    return out


def family(key, op, removed):
    path = key.split(" ", 1)[1]
    for prefix, fam in FAMILY_PREFIXES:
        if path == prefix or path.startswith(prefix + "/"):
            return fam
    return "legacy" if op["deprecated"] and not removed else None


def documented_floor(key):
    path = key.split(" ", 1)[1]
    floors = [v for k, v in DOCUMENTED_FLOORS.items()
              if k == key or (k.endswith("*") and path.startswith(k[:-1]))]
    return min((tuple(int(x) for x in f.split(".")) for f in floors), default=None)


def build(repo):
    tags = release_tags(repo)
    cache, history = {}, {}
    for tag, blob in tags:
        if blob not in cache:
            cache[blob] = operations(load_spec(repo, blob))
        for key, op in cache[blob].items():
            h = history.setdefault(key, {"present": []})
            h["present"].append(tag)
            h["op"] = op  # the last spec that contains it wins
    order = [t for t, _ in tags]
    newest = order[-1]
    paths, gaps = {}, []
    for key, h in history.items():
        first, last = h["present"][0], h["present"][-1]
        span = order[order.index(first):order.index(last) + 1]
        if len(span) != len(h["present"]):
            gaps.append(f"{key} absent from {len(span) - len(h['present'])} tags between {first} and {last}")
        op = dict(h["op"])
        if not op["operationId"]:
            sys.exit(f"{key} has no operationId in {last}")
        introduced = version(first)
        floor = documented_floor(key)
        if floor and floor < introduced:
            introduced = floor
        op["x-introduced"] = text(introduced)
        if last != newest:
            op["x-removed"] = text(version(order[order.index(last) + 1]))
        fam = family(key, op, "x-removed" in op)
        if fam:
            op["x-family"] = fam
        del op["deprecated"]
        method, path = key.split(" ", 1)
        paths.setdefault(path, {})[method.lower()] = op
    check_ids(paths)
    for g in gaps:
        print(f"note: {g} (the range ignores the gap)", file=sys.stderr)
    return {
        "x-generated-by": "scripts/gen-union-catalog.py",
        "x-oldest-version": text(version(order[0])),
        "x-newest-version": text(version(newest)),
        "x-releases": len(order),
        "x-distinct-specs": len(cache),
        "paths": paths,
    }


def check_ids(paths):
    """Two operations may share an operationId only when their ranges are disjoint."""
    by_id = {}
    for path, item in paths.items():
        for method, op in item.items():
            by_id.setdefault(op["operationId"], []).append((method.upper(), path, op))
    for oid, ops in by_id.items():
        ops.sort(key=lambda x: version("v" + x[2]["x-introduced"]))
        for a, b in zip(ops, ops[1:]):
            removed = a[2].get("x-removed")
            if removed is None or version("v" + removed) > version("v" + b[2]["x-introduced"]):
                sys.exit(f"operationId {oid} names overlapping operations: {a[0]} {a[1]} and {b[0]} {b[1]}")


def main():
    cache_home = os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache")
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--repo", default=os.path.join(cache_home, "langfuse-api-mcp", "langfuse.git"))
    ap.add_argument("--out", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "..",
                                                  "internal", "catalog", "spec", "langfuse-union-catalog.json"))
    args = ap.parse_args()
    refresh(args.repo)
    catalog = build(args.repo)
    with open(args.out, "w", encoding="utf-8", newline="\n") as f:
        json.dump(catalog, f, indent=1, sort_keys=True, ensure_ascii=False)
        f.write("\n")
    count = sum(len(v) for v in catalog["paths"].values())
    print(f"{count} operations from {catalog['x-releases']} releases "
          f"({catalog['x-distinct-specs']} distinct specs), "
          f"v{catalog['x-oldest-version']} to v{catalog['x-newest-version']}", file=sys.stderr)


if __name__ == "__main__":
    main()
