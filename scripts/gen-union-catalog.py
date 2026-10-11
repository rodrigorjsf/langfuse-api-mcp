#!/usr/bin/env python3
#
# WHAT: builds the union catalog (ADR-0012 §1) from the OpenAPI spec committed at
#       every Langfuse release tag from v3.0.0 onward, and writes it to
#       internal/catalog/spec/langfuse-union-catalog.json, which the server embeds.
#       Each operation (method + path) keeps its definition from the last spec
#       that contained it, except for the request body and the parameters. Its
#       JSON request bodies are every distinct body any release spec gave it
#       (x-request-bodies, #202): each with its $refs inlined from the spec of
#       the release it was taken from (#81), converted from the OpenAPI 3.0
#       schema dialect to JSON Schema 2020-12 (#111: nullable, boolean
#       exclusive bounds, example), so the server can compile it with
#       jsonschema-go, and the releases it holds for (x-spans: a run of
#       consecutive releases from x-introduced up to, not including,
#       x-removed; a body that comes back after another is stored once, with
#       both spans). Two bodies that differ only in the description and title
#       annotations are one body, with the newest text. The server picks the
#       body of the deployment's version, so an older deployment is checked
#       against the body its own release spec has. An operation the server
#       never offers (NEWEST_BODY_ONLY, ADR-0004) keeps its newest body only,
#       without spans: its history is size the server cannot use. Its path and query parameters are the union over
#       every release, each with the release that introduced it (x-introduced)
#       and, when it left the spec while the operation stayed, the release that
#       removed it (x-removed, #173), so that the server can offer the
#       parameters a deployment's version serves. It also carries
#       its version range (x-introduced, plus x-removed when it left the spec)
#       and its operation family (x-family: legacy, v4 read or experiments)
#       where a write mode gates it. An operation whose description opens
#       with a deprecation notice also gets x-summary, its operation index
#       line: what it does and the operation to prefer (ADR-0012 §5, #79).
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
# Needs git, network access to github.com and PyYAML
# (pip install --require-hashes -r scripts/requirements.txt, hash-pinned, #92):
# the release specs are YAML, and parsing them in Go would add a module
# dependency to the server for a maintainer-only step (security.md: no new
# dependency without justification).
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
            if not isinstance(target, dict) or part not in target:
                sys.exit(f"unresolvable reference {ref}")
            target = target[part]
        if not isinstance(target, dict):
            sys.exit(f"unresolvable reference {ref}")
        merged = deref(target, spec, seen + (ref,))
        rest = {k: deref(v, spec, seen) for k, v in node.items() if k != "$ref"}
        if merged.get("nullable") or rest.get("nullable"):
            rest["nullable"] = True
        return {**merged, **rest}
    return {k: deref(v, spec, seen) for k, v in node.items()}


def operations(spec):
    """{"METHOD path": operation} of one spec, parameters and request body inlined.

    Kept: the path and query parameters the read path uses, and the JSON
    request body of a write operation, which execute_write's body gate (M4)
    checks against (#81). Both are resolved against this spec's own components,
    so an operation keeps the schemas of the release it was taken from. Header
    parameters, responses and a parameter whose schema is an object (is_object)
    are dropped: nothing uses them.
    """
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
            kept = [p for p in params.values() if p.get("in") in ("path", "query") and not is_object(p)]
            for p in kept:
                p.pop("description", None)
                if isinstance(p.get("schema"), dict):
                    p["schema"].pop("description", None)
            key = f"{method.upper()} {path}"
            out[key] = {
                "operationId": op.get("operationId"),
                "tags": op.get("tags") or [],
                "description": op.get("description") or op.get("summary") or "",
                "deprecated": bool(op.get("deprecated")),
                "parameters": kept,
            }
            if "requestBody" in op:
                out[key]["requestBody"] = request_body(key, op["requestBody"], spec)
    return out


def is_object(p):
    """Whether parameter p is an object, which no URL parameter of the API is.

    Releases 3.57.1 to 3.78.x list the response schema of datasetRunItems_list
    as a required query parameter named "response": a spec error. Kept, it
    would be a required parameter of the operation on those deployments (#173).
    """
    schema = p.get("schema")
    return isinstance(schema, dict) and (schema.get("type") == "object" or "properties" in schema)


def request_body(key, body, spec):
    """The operation's request body, $refs inlined from spec: a JSON body only.

    A $ref that is cyclic, points outside components or names nothing fails
    generation (deref), so no schema is ever cut short or left half resolved.
    """
    body = deref(body, spec)
    content = body.get("content") if isinstance(body, dict) else None
    if not isinstance(content, dict) or set(content) != {"application/json"} \
            or not isinstance(content["application/json"].get("schema"), dict):
        sys.exit(f"{key}: request body is not a single JSON schema")
    return {"required": bool(body.get("required")),
            "content": {"application/json": {"schema": content["application/json"]["schema"]}}}


def body_2020_12(key, body):
    """body, a request body of operations(), with its schema in JSON Schema
    2020-12 (to_2020_12). merge converts the body of every release (#202), so a
    keyword with no conversion in any release's body stops generation: no body
    is dropped, cut or left unconverted to make generation pass."""
    try:
        schema = to_2020_12(body["content"]["application/json"]["schema"])
    except ValueError as e:
        sys.exit(f"{key}: request body schema: {e}")
    return {**body, "content": {"application/json": {"schema": schema}}}


# The operations the server never offers on any deployment (ADR-0004 and its M4
# amendment): they keep their newest body only. This is the list `excluded` in
# internal/catalog/catalog.go; TestEveryOperationTheServerOffersKeepsItsBodyHistory
# fails when an operation the server offers is marked x-newest-body-only.
NEWEST_BODY_ONLY = (
    "ingestion_batch", "opentelemetry_exportTraces", "projects_create", "projects_update", "projects_delete",
    "projects_createApiKey", "projects_deleteApiKey", "organizations_updateOrganizationMembership",
    "organizations_deleteOrganizationMembership", "organizations_updateProjectMembership",
    "organizations_deleteProjectMembership", "scim_createUser", "scim_deleteUser", "media_getUploadUrl",
    "media_patch", "llmConnections_upsert", "blobStorageIntegrations_upsertBlobStorageIntegration",
)

# The keywords of JSON Schema 2020-12 whose value is a schema, a list of schemas
# or an object of named schemas: the places an annotation is looked for. Every
# other keyword's value is data (const, enum, default, examples) or an
# extension, and so is a property name. Mirrors dropAnnotations in
# internal/catalog/annotations.go, the rule the discovery cap (#190) uses.
ANNOTATION_SCHEMAS = ("additionalItems", "additionalProperties", "contains", "contentSchema", "else", "if", "items",
                      "not", "propertyNames", "then", "unevaluatedItems", "unevaluatedProperties")
ANNOTATION_LISTS = ("allOf", "anyOf", "oneOf", "prefixItems")
ANNOTATION_MAPS = ("$defs", "definitions", "dependentSchemas", "patternProperties", "properties")


def without_annotations(schema):
    """schema without the description and title annotations of itself and of
    every subschema: what two bodies are compared by (#202). A property named
    description or title is a property, not an annotation, and nothing inside
    const, enum, default or examples is touched."""
    if not isinstance(schema, dict):
        return schema  # a boolean schema
    out = {k: v for k, v in schema.items() if k not in ANNOTATIONS}
    for k, v in out.items():
        if k in ANNOTATION_SCHEMAS:
            out[k] = without_annotations(v)
        elif k in ANNOTATION_LISTS and isinstance(v, list):
            out[k] = [without_annotations(x) for x in v]
        elif k in ANNOTATION_MAPS and isinstance(v, dict):
            out[k] = {name: without_annotations(x) for name, x in v.items()}
    return out


def body_signature(body):
    """What makes a converted body, body_2020_12's result, the same as another:
    its required flag and its schema, annotations apart."""
    return json.dumps({"required": body["required"],
                       "schema": without_annotations(body["content"]["application/json"]["schema"])}, sort_keys=True)


# Keywords whose value is a map of property name to schema, a schema, or a list
# of schemas. Every other keyword's value is data (enum, const, default), never
# walked: a property named "nullable" or an enum value "nullable" stays.
SCHEMA_MAPS = ("properties",)
SCHEMA_VALUES = ("items", "additionalProperties", "not")
SCHEMA_LISTS = ("allOf", "anyOf", "oneOf")
ANNOTATIONS = ("title", "description")
# The OpenAPI 3.0 Schema Object keywords JSON Schema 2020-12 does not have,
# other than nullable, the boolean exclusive bounds and example, which
# to_2020_12 converts. No release spec's body schema holds one (v3.0.0 to
# v4.50.0); one that appears stops generation, so its conversion is a decision,
# not a guess.
UNCONVERTED = ("discriminator", "xml", "externalDocs")


def to_2020_12(schema):
    """schema, an OpenAPI 3.0 schema object, as a JSON Schema 2020-12 one (#111).

    nullable: true adds null to the schema's type, and to its enum values. A
    schema with no single type, or whose const or combinator would still refuse
    null, becomes the alternative of itself and null, its title and description
    kept outside; one that constrains nothing already accepts null and only
    loses the keyword.
    A boolean exclusiveMinimum/exclusiveMaximum becomes the numeric form: true
    moves the minimum/maximum into it, false is dropped. example, which JSON
    Schema 2020-12 does not have, becomes examples, a list of that one value
    (#202: the ingestion and score bodies of the first releases hold it); it
    changes no body's validity. A schema the
    conversion cannot express, or one holding an UNCONVERTED keyword, raises
    ValueError.
    """
    out = {}
    for k, v in schema.items():
        if k in UNCONVERTED:
            raise ValueError(f"OpenAPI 3.0 keyword {k} has no conversion; add one to to_2020_12")
        if k == "example":
            if "examples" in schema:
                raise ValueError("example next to examples has no conversion; add one to to_2020_12")
            out["examples"] = [v]
            continue
        if k in SCHEMA_MAPS:
            v = {name: to_2020_12(s) for name, s in v.items()}
        elif k in SCHEMA_VALUES and isinstance(v, dict):
            v = to_2020_12(v)
        elif k in SCHEMA_LISTS:
            v = [to_2020_12(s) for s in v]
        out[k] = v
    for flag, bound in (("exclusiveMinimum", "minimum"), ("exclusiveMaximum", "maximum")):
        if isinstance(out.get(flag), bool):
            if out.pop(flag):
                if bound not in out:
                    raise ValueError(f"{flag} is true with no {bound}")
                out[flag] = out.pop(bound)
    if out.pop("nullable", False) is not True:
        return out
    if isinstance(out.get("type"), str) and not any(k in out for k in ("const", "not") + SCHEMA_LISTS):
        out["type"] = [out["type"], "null"]
        if "enum" in out and None not in out["enum"]:
            out["enum"] = out["enum"] + [None]
        return out
    notes = {k: v for k, v in out.items() if k in ANNOTATIONS}
    rest = {k: v for k, v in out.items() if k not in ANNOTATIONS}
    if not rest:
        return out  # it accepts anything, null included
    return {**notes, "anyOf": [rest, {"type": "null"}]}


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
    cache = {}
    releases = []
    for tag, blob in release_tags(repo):
        if blob not in cache:
            cache[blob] = operations(load_spec(repo, blob))
        releases.append((tag, cache[blob]))
    return merge(releases)


def param_key(p):
    return (p.get("name"), p.get("in"))


def note_parameters(history, tag, ops):
    """Add the parameters of release tag's operations to their histories: for
    every path and query parameter, the releases that list it and its
    definition from the last of them (#173)."""
    for key, op in ops.items():
        seen = history[key].setdefault("params", {})
        for p in op["parameters"]:
            h = seen.setdefault(param_key(p), {"present": [], "order": len(seen)})
            h["present"].append(tag)
            h["param"] = p  # the last spec that lists it wins


def parameters(h, order, introduced, notes):
    """The operation's parameters across every release (#173): those of the
    newest spec that lists the operation first, in its order, then the ones
    only older releases had, by first appearance. Each carries x-introduced,
    the release that introduced it, and x-removed, the release that removed it
    while the operation stayed; a parameter that lived as long as the
    operation takes the operation's range, so the documented floors of an
    operation also reach its parameters. A release in between that lacks a
    parameter is ignored, as for operations; it is added to notes."""
    first, last = h["present"][0], h["present"][-1]
    listed = [param_key(p) for p in h["op"]["parameters"]]
    rest = sorted((k for k in h["params"] if k not in listed), key=lambda k: h["params"][k]["order"])
    out = []
    for k in listed + rest:
        ph = h["params"][k]
        pfirst, plast = ph["present"][0], ph["present"][-1]
        lacking = order.index(plast) - order.index(pfirst) + 1 - len(ph["present"])
        if lacking:
            notes.append(f"parameter {k[0]} ({k[1]}) of {h['key']} absent from {lacking} tags between {pfirst} and {plast}")
        p = dict(ph["param"])
        p["x-introduced"] = introduced if pfirst == first else text(version(pfirst))
        if plast != last:
            p["x-removed"] = text(version(order[order.index(plast) + 1]))
        out.append(p)
    return out


def request_bodies(h, order, introduced, converted):
    """The operation's distinct request bodies across every release (#202),
    oldest first, each the body_2020_12 of its newest release's text plus
    x-spans: the runs of consecutive releases it holds for, from the release
    that introduced it up to, not including, the release that changed it.

    Releases are compared in the order they list the operation; one that lacks
    it is ignored, as for operations and parameters (the gap belongs to the run
    around it). A release that lists the operation without a body ends the body
    before it. The first span of an operation starts at the operation's own
    x-introduced, so a documented earlier floor also reaches its body. The last
    body stays open-ended even when the operation left the spec: the
    operation's x-removed bounds it. converted caches the conversion of each
    release's body, since releases share their spec's operations."""
    runs = []  # [signature or None, first release, body of the run's newest release]
    for tag, raw in h["bodies"]:
        if raw is None:
            sig, body = None, None
        else:
            if id(raw) not in converted:
                converted[id(raw)] = body_2020_12(h["key"], raw)
            body = converted[id(raw)]
            sig = body_signature(body)
        if runs and runs[-1][0] == sig:
            runs[-1][2] = body  # the newest text of the run
        else:
            runs.append([sig, tag, body])
    distinct = {}
    for i, (sig, first, body) in enumerate(runs):
        if sig is None:
            continue
        span = {"x-introduced": introduced if i == 0 else text(version(first))}
        if i + 1 < len(runs):
            span["x-removed"] = text(version(runs[i + 1][1]))
        entry = distinct.setdefault(sig, {"spans": []})
        entry["body"] = body  # a later run holds the newer text
        entry["spans"].append(span)
    return [{**e["body"], "x-spans": e["spans"]} for e in distinct.values()]


def merge(releases):
    """The union catalog of releases, [(tag, operations(spec))] oldest first."""
    history = {}
    for tag, ops in releases:
        for key, op in ops.items():
            h = history.setdefault(key, {"present": [], "key": key, "bodies": []})
            h["present"].append(tag)
            h["bodies"].append((tag, op.get("requestBody")))
            h["op"] = op  # the last spec that contains it wins
        note_parameters(history, tag, ops)
    converted = {}
    order = [t for t, _ in releases]
    newest = order[-1]
    paths, gaps = {}, []
    for key, h in history.items():
        first, last = h["present"][0], h["present"][-1]
        span = order[order.index(first):order.index(last) + 1]
        if len(span) != len(h["present"]):
            gaps.append(f"{key} absent from {len(span) - len(h['present'])} tags between {first} and {last}")
        op = dict(h["op"])
        op.pop("requestBody", None)
        if not op["operationId"]:
            sys.exit(f"{key} has no operationId in {last}")
        introduced = version(first)
        floor = documented_floor(key)
        if floor and floor < introduced:
            introduced = floor
        op["x-introduced"] = text(introduced)
        bodies = request_bodies(h, order, op["x-introduced"], converted)
        if op["operationId"] in NEWEST_BODY_ONLY and bodies:
            bodies = [{k: v for k, v in bodies[-1].items() if k != "x-spans"}]
            op["x-newest-body-only"] = True
        if bodies:
            op["x-request-bodies"] = bodies
        op["parameters"] = parameters(h, order, op["x-introduced"], gaps)
        if last != newest:
            op["x-removed"] = text(version(order[order.index(last) + 1]))
        fam = family(key, op, "x-removed" in op)
        if fam:
            op["x-family"] = fam
        del op["deprecated"]
        method, path = key.split(" ", 1)
        paths.setdefault(path, {})[method.lower()] = op
    check_ids(paths)
    summarize(paths)
    for g in gaps:
        print(f"note: {g} (the range ignores the gap)", file=sys.stderr)
    return {
        "x-generated-by": "scripts/gen-union-catalog.py",
        "x-oldest-version": text(version(order[0])),
        "x-newest-version": text(version(newest)),
        "x-releases": len(order),
        "x-distinct-specs": len({id(ops) for _, ops in releases}),
        "paths": paths,
    }


NOTICE = "**Deprecated:**"
NAMED_PATH = re.compile(r"(?:\b(GET|POST|PUT|PATCH|DELETE) )?(/api/public/[A-Za-z0-9_./{}-]*[A-Za-z0-9_}])")


def summarize(paths):
    """Give each operation whose description opens with a deprecation notice an
    x-summary: its operation index line (ADR-0012 §5, #79).

    The notice is a whole paragraph (about 500 characters, with a Markdown
    link) and the line saying what the operation does comes after it. The
    summary keeps that line and names the replacement the notice points to:
    "Get list of traces (legacy: prefer observations_getMany when it is
    available)". The replacement is the operation the newest spec lists with
    the same method at the named path (at its by-ID child when the legacy
    operation is by ID), else the path as the notice writes it.
    """
    live = {(m.upper(), p): op["operationId"]
            for p, item in paths.items() for m, op in item.items() if "x-removed" not in op}
    for path, item in paths.items():
        for method, op in item.items():
            notice, _, rest = op["description"].partition("\n")
            if not notice.startswith(NOTICE):
                continue
            what = next((line.strip() for line in rest.splitlines() if line.strip()), "")
            named = NAMED_PATH.search(notice)
            if named is None:
                op["x-summary"] = f"{what} (legacy)"
                continue
            verb, target = named.group(1), named.group(2)
            children = [p for (m, p) in live if m == method.upper() and re.fullmatch(re.escape(target) + r"/\{[^/]+\}", p)]
            if path.endswith("}") and len(children) == 1:
                target = children[0]
            written = f"{verb} {named.group(2)}" if verb else named.group(2)
            replacement = live.get((method.upper(), target)) or written
            op["x-summary"] = f"{what} (legacy: prefer {replacement} when it is available)"


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
    ap = argparse.ArgumentParser(description="Regenerate the union catalog from the Langfuse release specs since v3.0.0.")
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
