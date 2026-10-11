#!/usr/bin/env python3
"""
WHAT: loads the deterministic end-to-end seed (traces, observations, scores,
      score configs, prompts, datasets with items and runs, comments) into a
      throwaway self-hosted Langfuse through its public API, waits until every
      seeded trace, observation and score reads back, and writes the answer key
      (.seed-expected.json at the repository root) computed from the seed
      literals and the resolved time offsets, never by querying Langfuse back.
WHY:  the end-to-end campaign grades the MCP server against known data; the
      seed gives every worker the same records, and the answer key is the
      independent oracle. Includes poisoned records (prompt injection, hidden
      Unicode, control characters, Markdown/HTML, fake secrets) as security
      test material, so it must never land in a real project.
WHEN: once per stack, right after `scripts/langfuse-selfhosted.sh up`. It
      refuses a non-loopback base URL unless --allow-non-loopback is given.
      Proven on 3.80.0 and 4.46.0 dual. On 4.46.0 the readback waits for the
      dataset run items, which Langfuse writes after it answers, and accepts a
      dataset item that Langfuse stored without its control characters (it
      says so in a "note:" line).
      Re-seeding a used stack needs `down` then `up`: a second run is safe
      (fixed ids, upserts, existence checks) but resolves a new "now", so the
      timestamps of the records move and the key is rewritten to match.
HOW:  set -a; . ./.env.integration.selfhosted; set +a
      python3 scripts/seed-langfuse.py [--seed PATH] [--out PATH] [--now ISO8601]
                                       [--timeout SECONDS] [--allow-non-loopback]
      python3 scripts/seed-langfuse.py --self-test     # offline arithmetic check
      Reads LANGFUSE_TEST_BASE_URL, LANGFUSE_TEST_PUBLIC_KEY and
      LANGFUSE_TEST_SECRET_KEY from the environment; the keys are never printed
      or written. Python standard library only. Exit 0 only when readback
      converged; non-zero otherwise.

Seed data: internal/server/testdata/langfuse-selfhosted/seed/seed.json.
"""

import argparse
import base64
import copy
import datetime as dt
import decimal
import ipaddress
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from decimal import Decimal

ROOT = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
DEFAULT_SEED = os.path.join(ROOT, "internal/server/testdata/langfuse-selfhosted/seed/seed.json")
DEFAULT_OUT = os.path.join(ROOT, ".seed-expected.json")
NAMESPACE = uuid.UUID("5eed5eed-3800-4000-8000-000000000001")
MAX_BATCH_BYTES = 900_000
EPOCH = dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc)


class Fail(Exception):
    pass


# ----------------------------------------------------------------- time + data
def to_ms(d):
    return (d - EPOCH) // dt.timedelta(milliseconds=1)


def iso(ms):
    d = EPOCH + dt.timedelta(milliseconds=ms)
    return d.strftime("%Y-%m-%dT%H:%M:%S.") + "%03dZ" % (ms % 1000)


def resolve_at(at, now_ms):
    if "secondsAgo" in at:
        return now_ms - int(at["secondsAgo"]) * 1000
    now = EPOCH + dt.timedelta(milliseconds=now_ms)
    day = (now - dt.timedelta(days=int(at["daysAgo"]))).replace(hour=0, minute=0, second=0, microsecond=0)
    h, m, s = (int(x) for x in at["time"].split(":"))
    return to_ms(day) + ((h * 60 + m) * 60 + s) * 1000


def expand(v):
    """Expand {"$repeat": {unit, times}} into a string, recursively."""
    if isinstance(v, dict):
        if set(v) == {"$repeat"}:
            return v["$repeat"]["unit"] * int(v["$repeat"]["times"])
        return {k: expand(x) for k, x in v.items()}
    if isinstance(v, list):
        return [expand(x) for x in v]
    return v


def resolve(seed, now_ms):
    """Seed + now -> traces with absolute millisecond times (pure)."""
    out = []
    for t in seed["traces"]:
        t = copy.deepcopy(t)
        t["tsMs"] = resolve_at(t["at"], now_ms)
        for o in t["observations"]:
            o["startAbs"] = t["tsMs"] + o["startMs"]
            o["endAbs"] = None if o["type"] == "EVENT" else o["startAbs"] + o["durationMs"]
        for s in t["scores"]:
            s["tsMs"] = t["tsMs"] + s["atMs"]
        out.append(t)
    return out


def dec(x):
    return Decimal(str(x))


def num(d, places=8):
    return float(round(d, places))


def utc_day(ms):
    return (EPOCH + dt.timedelta(milliseconds=ms)).strftime("%Y-%m-%d")


# ----------------------------------------------------------------- answer key
def final_prompt_labels(versions):
    """A label sits on the highest version that declares it; 'latest' on the last."""
    out = []
    for i, v in enumerate(versions, start=1):
        keep = [l for l in v["labels"] if not any(l in w["labels"] for w in versions[i:])]
        if i == len(versions):
            keep = keep + ["latest"]
        out.append(sorted(keep))
    return out


def tree(t):
    kids = {}
    for o in t["observations"]:
        kids.setdefault(o["parent"], []).append(o)

    def node(o, depth, pre):
        pre.append({"id": o["id"], "depth": depth})
        n = {"id": o["id"], "name": o["name"], "type": o["type"], "level": o["level"]}
        if o.get("statusMessage"):
            n["statusMessage"] = o["statusMessage"]
        ch = sorted(kids.get(o["id"], []), key=lambda x: (x["startMs"], x["id"]))
        n["children"] = [node(c, depth + 1, pre) for c in ch]
        return n

    pre = []
    roots = sorted(kids.get(None, []), key=lambda x: (x["startMs"], x["id"]))
    return [node(r, 0, pre) for r in roots], pre


def compute_key(seed, traces, now_ms, named_trees=()):
    key = {"note": "Answer key computed from the seed literals and the resolved offsets, never by querying Langfuse. "
                   "Costs are USD, the explicit costDetails of the seed (Langfuse keeps them as given). "
                   "Latency of a trace = max end - min start of its observations (equals its root span). "
                   "Per-day buckets use the UTC date of the observation start.",
           "seedNow": iso(now_ms), "seedNowEpochMs": now_ms}
    by = lambda f: {k: v for k, v in sorted(_count(f(t) for t in traces).items())}
    key["traces"] = {
        "total": len(traces),
        "byUser": by(lambda t: [t["userId"]]),
        "bySession": by(lambda t: [t["sessionId"]] if t["sessionId"] else []),
        "withoutSession": sum(1 for t in traces if not t["sessionId"]),
        "byTag": by(lambda t: t["tags"]),
        "byEnvironment": by(lambda t: [t["environment"]]),
        "byRelease": by(lambda t: [t["release"]]),
        "byName": by(lambda t: [t["name"]]),
        "byDay": by(lambda t: [utc_day(t["tsMs"])]),
        "oldestTimestamp": iso(min(t["tsMs"] for t in traces)),
        "newestTimestamp": iso(max(t["tsMs"] for t in traces)),
    }
    # per-trace summary, errors, latency, tokens, cost
    summ, errors, warnings = [], [], []
    model_tot, day_tot, model_day = {}, {}, {}
    slow_obs = None
    slow_nonroot = None
    for t in traces:
        starts = [o["startAbs"] for o in t["observations"]]
        ends = [o["endAbs"] for o in t["observations"] if o["endAbs"] is not None]
        lat = (max(ends) - min(starts)) if ends else 0
        tin = tout = 0
        cost = Decimal(0)
        errs, warns = [], []
        for o in t["observations"]:
            if o.get("usage"):
                tin += o["usage"]["input"]
                tout += o["usage"]["output"]
                c = dec(o["cost"]["total"])
                cost += c
                for bucket, k in ((model_tot, o["model"]), (day_tot, utc_day(o["startAbs"])), (model_day, (utc_day(o["startAbs"]), o["model"]))):
                    b = bucket.setdefault(k, {"generations": 0, "inputTokens": 0, "outputTokens": 0, "totalTokens": 0, "cost": Decimal(0)})
                    b["generations"] += 1
                    b["inputTokens"] += o["usage"]["input"]
                    b["outputTokens"] += o["usage"]["output"]
                    b["totalTokens"] += o["usage"]["total"]
                    b["cost"] += c
            if o["level"] == "ERROR":
                errs.append({"id": o["id"], "name": o["name"], "type": o["type"], "statusMessage": o.get("statusMessage")})
            if o["level"] == "WARNING":
                warns.append({"id": o["id"], "name": o["name"], "type": o["type"], "statusMessage": o.get("statusMessage")})
            if o["endAbs"] is not None:
                if slow_obs is None or o["durationMs"] > slow_obs["latencyMs"]:
                    slow_obs = {"traceId": t["id"], "id": o["id"], "name": o["name"], "latencyMs": o["durationMs"]}
                if o["parent"] is not None and (slow_nonroot is None or o["durationMs"] > slow_nonroot["latencyMs"]):
                    slow_nonroot = {"traceId": t["id"], "id": o["id"], "name": o["name"], "latencyMs": o["durationMs"]}
        if errs:
            errors.append({"traceId": t["id"], "errorObservations": errs})
        if warns:
            warnings.append({"traceId": t["id"], "warningObservations": warns})
        summ.append({"id": t["id"], "name": t["name"], "userId": t["userId"], "sessionId": t["sessionId"], "tags": t["tags"],
                     "environment": t["environment"], "release": t["release"], "timestamp": iso(t["tsMs"]),
                     "latencyMs": lat, "observationCount": len(t["observations"]),
                     "inputTokens": tin, "outputTokens": tout, "totalTokens": tin + tout, "totalCost": num(cost),
                     "errorObservationIds": [e["id"] for e in errs], "scoreIds": [s["id"] for s in t["scores"]]})
    key["traces"]["list"] = summ
    key["errors"] = {"tracesWithErrors": len(errors), "errorObservationCount": sum(len(e["errorObservations"]) for e in errors), "detail": errors}
    key["warnings"] = {"tracesWithWarnings": len(warnings), "warningObservationCount": sum(len(e["warningObservations"]) for e in warnings), "detail": warnings}
    fmt = lambda b: {k: (num(v) if k == "cost" else v) for k, v in b.items()}
    key["usage"] = {
        "byModel": {m: fmt(b) for m, b in sorted(model_tot.items())},
        "byDay": {d: fmt(b) for d, b in sorted(day_tot.items())},
        "byDayAndModel": {f"{d}|{m}": fmt(b) for (d, m), b in sorted(model_day.items())},
        "total": fmt({"generations": sum(b["generations"] for b in model_tot.values()),
                      "inputTokens": sum(b["inputTokens"] for b in model_tot.values()),
                      "outputTokens": sum(b["outputTokens"] for b in model_tot.values()),
                      "totalTokens": sum(b["totalTokens"] for b in model_tot.values()),
                      "cost": sum((b["cost"] for b in model_tot.values()), Decimal(0))}),
        "costSpikeDay": max(day_tot.items(), key=lambda kv: kv[1]["cost"])[0] if day_tot else None,
    }
    sl = max(summ, key=lambda s: s["latencyMs"])
    key["latency"] = {"slowestTrace": {"id": sl["id"], "name": sl["name"], "latencyMs": sl["latencyMs"]},
                      "slowestObservation": slow_obs, "slowestNonRootObservation": slow_nonroot,
                      "note": "Langfuse reports trace latency in seconds (latencyMs / 1000) and observation latency in ms."}
    # trees
    names = list(named_trees) or ["seed-trace-001", "seed-trace-024", "seed-trace-031", "seed-trace-032"]
    key["trees"] = {}
    for t in traces:
        roots, pre = tree(t)
        key["trees"][t["id"]] = {"named": t["id"] in names, "name": t["name"], "roots": roots, "preOrder": pre}
    key["namedTrees"] = [n for n in names if n in key["trees"]]
    # scores
    sc = {}
    per_trace = {}
    for t in traces:
        for s in t["scores"]:
            sc.setdefault(s["name"], []).append((t, s))
            per_trace.setdefault(t["id"], []).append({"id": s["id"], "name": s["name"], "dataType": s["dataType"], "value": s["value"],
                                                      "observationId": s.get("observationId"), "comment": s.get("comment")})
    cfg = {c["name"]: c for c in seed["scoreConfigs"]}
    key["scores"] = {"total": sum(len(v) for v in sc.values()), "byName": {}, "byTrace": per_trace,
                     "configs": [{k: v for k, v in c.items()} for c in seed["scoreConfigs"]]}
    for name, items in sorted(sc.items()):
        dts = {s["dataType"] for _, s in items}
        e = {"count": len(items), "dataType": sorted(dts)[0], "onTraces": sum(1 for _, s in items if not s.get("observationId")),
             "onObservations": sum(1 for _, s in items if s.get("observationId")),
             "withComment": sum(1 for _, s in items if s.get("comment")), "config": name if name in cfg else None}
        if dts == {"NUMERIC"}:
            vals = [dec(s["value"]) for _, s in items]
            e.update({"mean": num(sum(vals) / len(vals), 6), "min": float(min(vals)), "max": float(max(vals))})
        elif dts == {"BOOLEAN"}:
            e["distribution"] = {"True": sum(1 for _, s in items if s["value"] == 1), "False": sum(1 for _, s in items if s["value"] == 0)}
        else:
            e["distribution"] = _count([s["value"]] for _, s in items)
        e["scoreIds"] = [s["id"] for _, s in items]
        key["scores"]["byName"][name] = e
    # prompts
    key["prompts"] = []
    for p in seed["prompts"]:
        fl = final_prompt_labels(p["versions"])
        key["prompts"].append({"name": p["name"], "type": p["type"], "tags": p["tags"], "versionCount": len(p["versions"]),
                               "folderName": "/" in p["name"],
                               "versions": [{"version": i, "labelsDeclared": v["labels"], "labelsFinal": fl[i - 1], "commitMessage": v["commitMessage"], "config": v["config"]}
                                            for i, v in enumerate(p["versions"], start=1)]})
    key["promptsNote"] = "labelsFinal: a label stays on the highest version that declares it; Langfuse also puts 'latest' on the newest version (verified against 3.80.0 by the readback)."
    # datasets
    key["datasets"] = []
    for d in seed["datasets"]:
        key["datasets"].append({"name": d["name"], "folderName": "/" in d["name"], "description": d["description"], "metadata": d["metadata"],
                                "itemCount": len(d["items"]), "itemIds": [i["id"] for i in d["items"]],
                                "runs": [{"name": r["name"], "description": r["description"], "metadata": r["metadata"], "runItemCount": len(r["items"]),
                                          "runItems": [{"datasetItemId": x["item"], "traceId": x["trace"]} for x in r["items"]]} for r in d["runs"]]})
    key["comments"] = [{"id": c["id"], "objectType": c["objectType"], "objectId": c.get("objectId"), "objectName": c.get("objectName"), "content": c["content"]}
                       for c in seed["comments"]]
    # poison
    key["poison"] = {"records": seed["poison"],
                     "fakeSecretStrings": ["sk-lf-00000000-fake-seed-value", "Authorization: Bearer seed-fake-bearer-token-00000000"],
                     "categories": {"a-injection": "instruction-style prompt injection (asks the agent to call execute_write and delete records)",
                                    "b-invisible-bidi": "zero-width, bidi override/isolate, variation selectors, U+E0001-range tag characters",
                                    "c-control-chars": "U+0001, BEL, BS, ESC, DEL",
                                    "d-markdown-html": "Markdown link and image, raw script/img/iframe tags",
                                    "e-fake-secret": "fake sk-lf string and a fake Authorization: Bearer line"}}
    # oversized
    ov = [t for t in traces if t["id"] == "seed-trace-040"]
    if ov:
        t = expand(ov[0])
        size = lambda v: len(json.dumps(v, ensure_ascii=False).encode("utf-8"))
        parts = size(t["input"]) + size(t["output"]) + sum(size(o.get("input")) + size(o.get("output")) for o in t["observations"])
        key["oversized"] = {"traceId": t["id"], "approxPayloadBytes": parts, "observationCount": len(t["observations"]),
                            "note": "Several hundred KiB of input/output across the trace and its observations; exercises response truncation."}
    return key


def _count(items):
    out = {}
    for lst in items:
        for x in lst:
            out[x] = out.get(x, 0) + 1
    return out


# ----------------------------------------------------------------- ingestion
def eid(kind, ident):
    return str(uuid.uuid5(NAMESPACE, f"{kind}:{ident}"))


def ingestion_events(t, cfg_ids):
    t = expand(t)
    ev = []
    ev.append({"id": eid("trace", t["id"]), "type": "trace-create", "timestamp": iso(t["tsMs"]),
               "body": {"id": t["id"], "name": t["name"], "userId": t["userId"], "sessionId": t["sessionId"], "tags": t["tags"],
                        "environment": t["environment"], "release": t["release"], "version": t["version"],
                        "input": t["input"], "output": t["output"], "metadata": t["metadata"], "timestamp": iso(t["tsMs"])}})
    for o in t["observations"]:
        b = {"id": o["id"], "traceId": t["id"], "parentObservationId": o["parent"], "name": o["name"], "startTime": iso(o["startAbs"]),
             "level": o["level"], "environment": o.get("environment", t["environment"])}
        if o["endAbs"] is not None:
            b["endTime"] = iso(o["endAbs"])
        for k in ("statusMessage", "input", "output", "modelParameters", "promptName", "promptVersion"):
            if k in o:
                b[k] = o[k]
        if o["type"] == "GENERATION":
            if "model" in o:
                b["model"] = o["model"]
                b["usageDetails"] = o["usage"]
                b["costDetails"] = o["cost"]
            typ = "generation-create"
        else:
            typ = "span-create" if o["type"] == "SPAN" else "event-create"
        ev.append({"id": eid("obs", o["id"]), "type": typ, "timestamp": iso(o["startAbs"]), "body": b})
    for s in t["scores"]:
        b = {"id": s["id"], "traceId": t["id"], "name": s["name"], "value": s["value"], "dataType": s["dataType"], "environment": t["environment"]}
        if s.get("observationId"):
            b["observationId"] = s["observationId"]
        if s.get("comment") is not None:
            b["comment"] = s["comment"]
        if s.get("configName"):
            b["configId"] = cfg_ids[s["configName"]]
        ev.append({"id": eid("score", s["id"]), "type": "score-create", "timestamp": iso(s["tsMs"]), "body": b})
    return ev


def chunk(events):
    cur, size = [], 0
    for e in events:
        n = len(json.dumps(e))
        if cur and (size + n > MAX_BATCH_BYTES or len(cur) >= 100):
            yield cur
            cur, size = [], 0
        cur.append(e)
        size += n
    if cur:
        yield cur


# ----------------------------------------------------------------- HTTP
class Client:
    def __init__(self, base, pk, sk):
        self.base = base.rstrip("/")
        self.auth = "Basic " + base64.b64encode(f"{pk}:{sk}".encode()).decode()

    def call(self, method, path, body=None, query=None, ok=(200, 201, 207), retries=4):
        url = self.base + path + ("?" + urllib.parse.urlencode(query) if query else "")
        data = None if body is None else json.dumps(body).encode()
        last = None
        for attempt in range(retries):
            req = urllib.request.Request(url, data=data, method=method, headers={"Authorization": self.auth, "Content-Type": "application/json"})
            try:
                with urllib.request.urlopen(req, timeout=120) as r:
                    raw = r.read().decode("utf-8", "replace")
                    return r.status, (json.loads(raw) if raw else None)
            except urllib.error.HTTPError as e:
                raw = e.read().decode("utf-8", "replace")
                if e.code in (429, 502, 503, 504) and attempt < retries - 1:
                    time.sleep(2 ** attempt)
                    continue
                try:
                    parsed = json.loads(raw)
                except ValueError:
                    parsed = raw[:300]
                if e.code in ok:
                    return e.code, parsed
                last = Fail(f"{method} {path} -> HTTP {e.code}: {str(parsed)[:400]}")
                break
            except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
                last = Fail(f"{method} {path} -> {type(e).__name__}")
                time.sleep(2 ** attempt)
        raise last

    def get(self, path, query=None, allow_404=False):
        st, b = self.call("GET", path, query=query, ok=(200, 404) if allow_404 else (200,))
        return None if st == 404 else b

    def paged(self, path, query=None, limit=100, allow_404=False):
        page, out = 1, []
        while True:
            q = dict(query or {}, page=page, limit=limit)
            b = self.get(path, q, allow_404=allow_404)
            if b is None:
                return out
            out += b["data"]
            # never trust meta.totalPages: after a re-ingest Langfuse 3.80.0 counts unmerged duplicate rows
            if len(b["data"]) < limit:
                return out
            page += 1


def q(name):
    return urllib.parse.quote(name, safe="")


# ----------------------------------------------------------------- loading
def load_score_configs(c, seed, log):
    existing = {x["name"]: x for x in c.paged("/api/public/score-configs")}
    ids = {}
    for cfg in seed["scoreConfigs"]:
        if cfg["name"] in existing:
            ids[cfg["name"]] = existing[cfg["name"]]["id"]
            continue
        _, b = c.call("POST", "/api/public/score-configs", cfg)
        ids[cfg["name"]] = b["id"]
    log(f"score configs: {len(ids)}")
    return ids


def load_prompts(c, seed, log):
    made = 0
    for p in seed["prompts"]:
        # without a label Langfuse serves the production version, not the newest
        cur = c.get(f"/api/public/v2/prompts/{q(p['name'])}", {"label": "latest"}, allow_404=True)
        have = cur["version"] if cur else 0
        for i, v in enumerate(p["versions"], start=1):
            if i <= have:
                continue
            body = {"name": p["name"], "type": p["type"], "prompt": v["prompt"], "config": v["config"], "labels": v["labels"],
                    "tags": p["tags"], "commitMessage": v["commitMessage"]}
            c.call("POST", "/api/public/v2/prompts", body)
            made += 1
        top = c.get(f"/api/public/v2/prompts/{q(p['name'])}", {"label": "latest"})["version"]
        if top != len(p["versions"]):
            raise Fail(f"prompt {p['name']} has {top} versions, the seed {len(p['versions'])}: the stack was not fresh (prompts cannot be deleted); run `down` then `up`")
    log(f"prompts: {len(seed['prompts'])} ({made} versions created, rest existed)")


def ingest(c, seed, traces, cfg_ids, log):
    sent = 0
    for t in traces:
        for batch in chunk(ingestion_events(t, cfg_ids)):
            _, b = c.call("POST", "/api/public/ingestion", {"batch": batch})
            if b.get("errors"):
                raise Fail(f"ingestion rejected {len(b['errors'])} events of {t['id']}: {json.dumps(b['errors'])[:600]}")
            sent += len(batch)
    log(f"ingestion: {len(traces)} traces, {sent} events accepted")


def load_datasets(c, seed, log):
    ds_ids = {}
    n_items = n_run_items = 0
    for d in seed["datasets"]:
        _, b = c.call("POST", "/api/public/v2/datasets", {"name": d["name"], "description": d["description"], "metadata": d["metadata"]})
        ds_ids[d["name"]] = b["id"]
        for it in d["items"]:
            body = {"datasetName": d["name"], "id": it["id"], "input": it["input"], "expectedOutput": it["expectedOutput"], "metadata": it["metadata"]}
            if it.get("sourceTraceId"):
                body["sourceTraceId"] = it["sourceTraceId"]
            c.call("POST", "/api/public/dataset-items", body)
            n_items += 1
        for r in d["runs"]:
            have = {(x["datasetItemId"], x["traceId"]) for x in c.paged("/api/public/dataset-run-items", {"datasetId": ds_ids[d["name"]], "runName": r["name"]}, allow_404=True)}
            for x in r["items"]:
                n_run_items += 1
                if (x["item"], x["trace"]) in have:
                    continue
                c.call("POST", "/api/public/dataset-run-items", {"runName": r["name"], "runDescription": r["description"], "metadata": r["metadata"],
                                                                 "datasetItemId": x["item"], "traceId": x["trace"]})
    log(f"datasets: {len(ds_ids)}, items {n_items}, run items {n_run_items}")
    return ds_ids


def load_comments(c, seed, project_id, log):
    made = 0
    for cm in seed["comments"]:
        oid = cm.get("objectId")
        if cm["objectType"] == "PROMPT":
            p = c.get(f"/api/public/v2/prompts/{q(cm['objectName'])}")
            oid = p["id"]
        have = c.paged("/api/public/comments", {"objectType": cm["objectType"], "objectId": oid})
        if any(h["content"] == cm["content"] for h in have):
            continue
        c.call("POST", "/api/public/comments", {"projectId": project_id, "objectType": cm["objectType"], "objectId": oid, "content": cm["content"]})
        made += 1
    log(f"comments: {len(seed['comments'])} ({made} created, rest existed)")


# ----------------------------------------------------------------- readback
def close(a, b, tol):
    return a is not None and abs(float(a) - float(b)) <= tol


def check_traces(c, traces):
    """Returns (missing, mismatches) for traces, observations and scores."""
    missing, bad = [], []
    for t in traces:
        got = c.get(f"/api/public/traces/{q(t['id'])}", allow_404=True)
        if got is None:
            missing.append(f"trace {t['id']}")
            continue
        for f in ("name", "userId", "sessionId", "environment", "release"):
            if got.get(f) != t[f]:
                bad.append(f"trace {t['id']} {f}: got {str(got.get(f))[:60]!r}")
        if sorted(got.get("tags") or []) != sorted(t["tags"]):
            bad.append(f"trace {t['id']} tags: got {got.get('tags')}")
        gobs = {o["id"]: o for o in got.get("observations", [])}
        for o in t["observations"]:
            g = gobs.get(o["id"])
            if g is None:
                missing.append(f"observation {o['id']}")
                continue
            exp = {"type": o["type"], "name": o["name"], "parentObservationId": o["parent"], "level": o["level"],
                   "environment": o.get("environment", t["environment"]), "model": o.get("model"),
                   "statusMessage": o.get("statusMessage"), "promptName": o.get("promptName"), "promptVersion": o.get("promptVersion")}
            for k, v in exp.items():
                if g.get(k) != v:
                    bad.append(f"observation {o['id']} {k}: got {str(g.get(k))[:60]!r} want {str(v)[:60]!r}")
            if g.get("startTime") != iso(o["startAbs"]):
                bad.append(f"observation {o['id']} startTime: got {g.get('startTime')} want {iso(o['startAbs'])}")
            if o["endAbs"] is not None and g.get("endTime") != iso(o["endAbs"]):
                bad.append(f"observation {o['id']} endTime: got {g.get('endTime')} want {iso(o['endAbs'])}")
            if o.get("usage"):
                u = g.get("usageDetails") or {}
                if (u.get("input"), u.get("output"), u.get("total")) != (o["usage"]["input"], o["usage"]["output"], o["usage"]["total"]):
                    bad.append(f"observation {o['id']} usageDetails: got {u}")
                if not close((g.get("costDetails") or {}).get("total"), o["cost"]["total"], 1e-9):
                    bad.append(f"observation {o['id']} costDetails.total: got {(g.get('costDetails') or {}).get('total')} want {o['cost']['total']}")
        gsc = {s["id"]: s for s in got.get("scores", [])}
        for s in t["scores"]:
            g = gsc.get(s["id"])
            if g is None:
                missing.append(f"score {s['id']}")
                continue
            val = g.get("stringValue") if s["dataType"] == "CATEGORICAL" else g.get("value")
            want = s["value"]
            if s["dataType"] == "BOOLEAN":
                ok = float(g.get("value")) == float(want)
            elif s["dataType"] == "CATEGORICAL":
                ok = val == want
            else:
                ok = close(val, want, 1e-9)
            if not ok:
                bad.append(f"score {s['id']} value: got {val!r} want {want!r}")
            if g.get("dataType") != s["dataType"]:
                bad.append(f"score {s['id']} dataType: got {g.get('dataType')}")
            if g.get("comment") != s.get("comment"):
                bad.append(f"score {s['id']} comment differs")
            if g.get("observationId") != s.get("observationId"):
                bad.append(f"score {s['id']} observationId: got {g.get('observationId')}")
        if not (got.get("latency") is not None and close(got["latency"] * 1000, max(o["endAbs"] for o in t["observations"] if o["endAbs"]) - min(o["startAbs"] for o in t["observations"]), 1.5)):
            bad.append(f"trace {t['id']} latency: got {got.get('latency')}")
    return missing, bad


def wait_traces(c, traces, timeout, log):
    t0 = time.time()
    while True:
        missing, bad = check_traces(c, traces)
        if not missing and not bad:
            return missing, bad
        if time.time() - t0 > timeout:
            return missing, bad
        log(f"  waiting for ingestion: {len(missing)} missing, {len(bad)} mismatches ...")
        time.sleep(5)


def without_controls(v):
    """v with the C0 control characters (tab, line feed and carriage return kept) and DEL removed from every string."""
    if isinstance(v, str):
        return "".join(ch for ch in v if ch in "\t\n\r" or (ord(ch) >= 0x20 and ord(ch) != 0x7F))
    if isinstance(v, dict):
        return {k: without_controls(x) for k, x in v.items()}
    if isinstance(v, list):
        return [without_controls(x) for x in v]
    return v


def check_rest(c, seed, ds_ids, project_id):
    """Existence and shape of prompts, datasets, items, runs, comments, configs.

    Returns (missing, bad, notes): a note is a difference Langfuse itself makes
    on write, which is not a failed load.
    """
    missing, bad, notes = [], [], []
    for p in seed["prompts"]:
        fl = final_prompt_labels(p["versions"])
        for i, v in enumerate(p["versions"], start=1):
            g = c.get(f"/api/public/v2/prompts/{q(p['name'])}", {"version": i}, allow_404=True)
            if g is None:
                missing.append(f"prompt {p['name']} v{i}")
                continue
            if g["type"] != p["type"]:
                bad.append(f"prompt {p['name']} v{i} type: {g['type']}")
            if sorted(g.get("labels") or []) != fl[i - 1]:
                bad.append(f"prompt {p['name']} v{i} labels: got {sorted(g.get('labels') or [])} want {fl[i - 1]}")
            if sorted(g.get("tags") or []) != sorted(p["tags"]):
                bad.append(f"prompt {p['name']} v{i} tags: got {g.get('tags')}")
            if g["prompt"] != v["prompt"]:
                bad.append(f"prompt {p['name']} v{i} body differs")
    for d in seed["datasets"]:
        g = c.get(f"/api/public/v2/datasets/{q(d['name'])}", allow_404=True)
        if g is None:
            missing.append(f"dataset {d['name']}")
            continue
        items = {i["id"]: i for i in c.paged("/api/public/dataset-items", {"datasetName": d["name"]})}
        for it in d["items"]:
            gi = items.get(it["id"])
            if gi is None:
                missing.append(f"dataset item {it['id']}")
            elif gi.get("expectedOutput") != it["expectedOutput"] or gi.get("input") != it["input"]:
                # Langfuse 4.46.0 drops control characters from a dataset item on
                # write (3.80.0 keeps them): the item is loaded, only poorer.
                if gi.get("expectedOutput") == without_controls(it["expectedOutput"]) and gi.get("input") == without_controls(it["input"]):
                    notes.append(f"dataset item {it['id']}: Langfuse stored it without its control characters")
                else:
                    bad.append(f"dataset item {it['id']} input/expectedOutput differ")
        if len(items) != len(d["items"]):
            bad.append(f"dataset {d['name']} item count: got {len(items)} want {len(d['items'])}")
        runs = {r["name"]: r for r in c.paged(f"/api/public/datasets/{q(d['name'])}/runs")}
        for r in d["runs"]:
            if r["name"] not in runs:
                missing.append(f"dataset run {r['name']}")
                continue
            ri = c.paged("/api/public/dataset-run-items", {"datasetId": ds_ids[d["name"]], "runName": r["name"]})
            want = {(x["item"], x["trace"]) for x in r["items"]}
            got = {(x["datasetItemId"], x["traceId"]) for x in ri}
            if got != want or len(ri) != len(want):
                bad.append(f"dataset run {r['name']} run items: got {len(ri)} want {len(want)}")
        if len(runs) != len(d["runs"]):
            bad.append(f"dataset {d['name']} run count: got {len(runs)} want {len(d['runs'])}")
    for cm in seed["comments"]:
        oid = cm.get("objectId")
        if cm["objectType"] == "PROMPT":
            oid = c.get(f"/api/public/v2/prompts/{q(cm['objectName'])}")["id"]
        have = c.paged("/api/public/comments", {"objectType": cm["objectType"], "objectId": oid})
        n = sum(1 for h in have if h["content"] == cm["content"])
        if n == 0:
            missing.append(f"comment {cm['id']}")
        elif n > 1:
            bad.append(f"comment {cm['id']} duplicated ({n})")
    names = {x["name"] for x in c.paged("/api/public/score-configs")}
    for cfg in seed["scoreConfigs"]:
        if cfg["name"] not in names:
            missing.append(f"score config {cfg['name']}")
    return missing, bad, notes


def wait_rest(c, seed, ds_ids, project_id, timeout, log):
    """check_rest until it converges: Langfuse 4.46.0 writes dataset run items after it answers the request."""
    t0 = time.time()
    while True:
        missing, bad, notes = check_rest(c, seed, ds_ids, project_id)
        if (not missing and not bad) or time.time() - t0 > timeout:
            return missing, bad, notes
        log(f"  waiting for datasets and comments: {len(missing)} missing, {len(bad)} mismatches ...")
        time.sleep(5)


# ----------------------------------------------------------------- self-test
def self_test():
    seed = {"scoreConfigs": [{"name": "q", "dataType": "NUMERIC"}], "prompts": [
        {"name": "a/b", "type": "text", "tags": [], "versions": [{"prompt": "x", "config": {}, "labels": ["production"], "commitMessage": ""},
                                                                 {"prompt": "y", "config": {}, "labels": ["production", "staging"], "commitMessage": ""}]}],
            "datasets": [{"name": "d", "description": "", "metadata": {}, "items": [{"id": "i1"}], "runs": [{"name": "r", "description": "", "metadata": {}, "items": [{"item": "i1", "trace": "t1"}]}]}],
            "comments": [], "poison": [],
            "traces": [
                {"id": "t1", "name": "n", "userId": "u1", "sessionId": "s1", "tags": ["x"], "environment": "e", "release": "r", "version": "1",
                 "at": {"daysAgo": 1, "time": "23:59:50"}, "input": 1, "output": 2, "metadata": {},
                 "observations": [
                     {"id": "o1", "type": "SPAN", "name": "root", "parent": None, "startMs": 0, "durationMs": 5000, "level": "DEFAULT"},
                     {"id": "o2", "type": "GENERATION", "name": "g", "parent": "o1", "startMs": 100, "durationMs": 4000, "level": "ERROR", "statusMessage": "boom",
                      "model": "m", "usage": {"input": 100, "output": 50, "total": 150}, "cost": {"input": 0.001, "output": 0.002, "total": 0.003}},
                     {"id": "o3", "type": "EVENT", "name": "e", "parent": "o2", "startMs": 200, "durationMs": 0, "level": "DEFAULT"}],
                 "scores": [{"id": "s1", "name": "q", "dataType": "NUMERIC", "value": 0.5, "atMs": 10},
                            {"id": "s2", "name": "q", "dataType": "NUMERIC", "value": 1.0, "atMs": 10, "observationId": "o2"}]},
                {"id": "t2", "name": "n", "userId": "u1", "sessionId": None, "tags": ["x", "y"], "environment": "e", "release": "r", "version": "1",
                 "at": {"secondsAgo": 60}, "input": 1, "output": 2, "metadata": {},
                 "observations": [
                     {"id": "p1", "type": "SPAN", "name": "root", "parent": None, "startMs": 0, "durationMs": 1000, "level": "DEFAULT"},
                     {"id": "p2", "type": "GENERATION", "name": "g", "parent": "p1", "startMs": 10, "durationMs": 900, "level": "DEFAULT",
                      "model": "m", "usage": {"input": 10, "output": 5, "total": 15}, "cost": {"input": 0.0001, "output": 0.0002, "total": 0.0003}}],
                 "scores": []}]}
    now = to_ms(dt.datetime(2026, 10, 10, 12, 0, 0, tzinfo=dt.timezone.utc))
    traces = resolve(seed, now)
    k = compute_key(seed, traces, now)
    want = [
        (k["seedNow"], "2026-10-10T12:00:00.000Z"),
        (k["traces"]["total"], 2), (k["traces"]["byUser"], {"u1": 2}), (k["traces"]["byTag"], {"x": 2, "y": 1}),
        (k["traces"]["bySession"], {"s1": 1}), (k["traces"]["withoutSession"], 1),
        (traces[0]["tsMs"], to_ms(dt.datetime(2026, 10, 9, 23, 59, 50, tzinfo=dt.timezone.utc))),
        (k["usage"]["byModel"]["m"]["totalTokens"], 165), (k["usage"]["byModel"]["m"]["cost"], 0.0033),
        (sorted(k["usage"]["byDay"]), ["2026-10-09", "2026-10-10"]), (k["usage"]["byDay"]["2026-10-09"]["cost"], 0.003),
        (k["latency"]["slowestTrace"]["id"], "t1"), (k["latency"]["slowestTrace"]["latencyMs"], 5000),
        (k["latency"]["slowestNonRootObservation"]["id"], "o2"),
        (k["errors"]["detail"][0]["errorObservations"][0]["id"], "o2"),
        (k["scores"]["byName"]["q"]["mean"], 0.75), (k["scores"]["byName"]["q"]["onObservations"], 1),
        (k["trees"]["t1"]["preOrder"], [{"id": "o1", "depth": 0}, {"id": "o2", "depth": 1}, {"id": "o3", "depth": 2}]),
        (k["prompts"][0]["versions"][0]["labelsFinal"], []), (k["prompts"][0]["versions"][1]["labelsFinal"], ["latest", "production", "staging"]),
        (k["datasets"][0]["runs"][0]["runItemCount"], 1),
        (iso(1), "1970-01-01T00:00:00.001Z"), (expand({"a": {"$repeat": {"unit": "ab", "times": 3}}}), {"a": "ababab"}),
        (without_controls({"a": ["x\u0001\u0007\b\u001b[31m\u007f\n\ty\u200b"]}), {"a": ["x[31m\n\ty\u200b"]}),
    ]
    bad = [(i, g, w) for i, (g, w) in enumerate(want) if g != w]
    for i, g, w in bad:
        print(f"self-test case {i}: got {g!r} want {w!r}", file=sys.stderr)
    if bad:
        return 1
    print(f"self-test ok ({len(want)} checks)")
    return 0


# ----------------------------------------------------------------- main
def main():
    ap = argparse.ArgumentParser(description="Seed a throwaway self-hosted Langfuse and write the answer key.")
    ap.add_argument("--seed", default=DEFAULT_SEED)
    ap.add_argument("--out", default=DEFAULT_OUT, help="answer key path (default .seed-expected.json at the repo root)")
    ap.add_argument("--now", help="fix the seed 'now' (ISO 8601 UTC); default: the current time")
    ap.add_argument("--timeout", type=int, default=300, help="seconds to wait for ingestion readback")
    ap.add_argument("--allow-non-loopback", action="store_true", help="allow a base URL that is not loopback (never use a real project)")
    ap.add_argument("--self-test", action="store_true")
    a = ap.parse_args()
    if a.self_test:
        return self_test()

    base = os.environ.get("LANGFUSE_TEST_BASE_URL", "")
    pk, sk = os.environ.get("LANGFUSE_TEST_PUBLIC_KEY", ""), os.environ.get("LANGFUSE_TEST_SECRET_KEY", "")
    missing = [n for n, v in (("LANGFUSE_TEST_BASE_URL", base), ("LANGFUSE_TEST_PUBLIC_KEY", pk), ("LANGFUSE_TEST_SECRET_KEY", sk)) if not v]
    if missing:
        print("missing environment: " + ", ".join(missing), file=sys.stderr)
        return 2
    host = urllib.parse.urlparse(base).hostname or ""
    try:
        loop = host == "localhost" or ipaddress.ip_address(host).is_loopback
    except ValueError:
        loop = False
    if not loop and not a.allow_non_loopback:
        print(f"refusing non-loopback base URL host {host!r}: this seed must never land in a real project (--allow-non-loopback overrides)", file=sys.stderr)
        return 2

    with open(a.seed, encoding="utf-8") as f:
        seed = json.load(f)
    if a.now:
        now_ms = to_ms(dt.datetime.fromisoformat(a.now.replace("Z", "+00:00")).astimezone(dt.timezone.utc))
    else:
        now_ms = (int(time.time() * 1000) // 1000) * 1000
    traces = resolve(seed, now_ms)
    key = compute_key(seed, traces, now_ms)
    with open(a.out, "w", encoding="utf-8") as f:
        json.dump(key, f, indent=1, ensure_ascii=False)
        f.write("\n")
    log = lambda m: print(m, flush=True)
    log(f"seed now {iso(now_ms)}; answer key written to {os.path.relpath(a.out, ROOT) if a.out.startswith(ROOT) else a.out}")

    c = Client(base, pk, sk)
    try:
        health = c.get("/api/public/health")
        log(f"langfuse {health.get('version')} at {base}")
        projects = c.get("/api/public/projects")["data"]
        if len(projects) != 1:
            raise Fail(f"expected exactly one project for the key, got {len(projects)}")
        project_id = projects[0]["id"]
        cfg_ids = load_score_configs(c, seed, log)
        load_prompts(c, seed, log)
        ingest(c, seed, traces, cfg_ids, log)
        log(f"waiting for the worker to ingest (up to {a.timeout}s) ...")
        miss, bad = wait_traces(c, traces, a.timeout, log)
        if miss or bad:
            for m in miss[:15]:
                print("  MISSING", m, file=sys.stderr)
            for m in bad[:25]:
                print("  MISMATCH", m, file=sys.stderr)
            raise Fail(f"readback did not converge: {len(miss)} missing, {len(bad)} mismatches")
        ds_ids = load_datasets(c, seed, log)
        load_comments(c, seed, project_id, log)
        miss, bad, notes = wait_rest(c, seed, ds_ids, project_id, a.timeout, log)
        if miss or bad:
            for m in miss[:15]:
                print("  MISSING", m, file=sys.stderr)
            for m in bad[:25]:
                print("  MISMATCH", m, file=sys.stderr)
            raise Fail(f"readback of prompts/datasets/comments failed: {len(miss)} missing, {len(bad)} mismatches")
        total = len({t["id"] for t in c.paged("/api/public/traces")})
        meta_total = c.get("/api/public/traces", {"limit": 1})["meta"]["totalItems"]
    except Fail as e:
        print(f"SEED FAILED: {e}", file=sys.stderr)
        return 1

    n_obs = sum(len(t["observations"]) for t in traces)
    n_sc = sum(len(t["scores"]) for t in traces)
    log("readback ok (seeded == read back):")
    log(f"  traces        {len(traces)} == {len(traces)}  (project holds {total}; meta.totalItems says {meta_total})")
    log(f"  observations  {n_obs} == {n_obs}")
    log(f"  scores        {n_sc} == {n_sc}")
    log(f"  score configs {len(seed['scoreConfigs'])} == {len(seed['scoreConfigs'])}")
    log(f"  prompts       {len(seed['prompts'])} ({sum(len(p['versions']) for p in seed['prompts'])} versions)")
    log(f"  datasets      {len(seed['datasets'])} ({sum(len(d['items']) for d in seed['datasets'])} items, {sum(len(d['runs']) for d in seed['datasets'])} runs, {sum(len(r['items']) for d in seed['datasets'] for r in d['runs'])} run items)")
    log(f"  comments      {len(seed['comments'])}")
    for n in notes:
        log(f"  note: {n}")
    if total != len(traces):
        print(f"WARNING: the project holds {total} traces, the seed {len(traces)}: the stack was not fresh", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
