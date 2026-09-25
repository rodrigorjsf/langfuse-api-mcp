# Build operation -> first/last release tag matrix from the OpenAPI spec committed at every release tag.
import subprocess, yaml, json, re, sys
from functools import lru_cache
tags = [l.split() for l in open("tagblob.txt")]
def ver(t): return tuple(int(x) for x in t[1:].split("."))
tags.sort(key=lambda x: ver(x[0]))
@lru_cache(None)
def ops(blob):
    src = subprocess.run(["git", "-C", "lfrepo.git", "cat-file", "-p", blob], capture_output=True, text=True).stdout
    try: spec = yaml.load(src, Loader=yaml.CSafeLoader)
    except AttributeError: spec = yaml.safe_load(src)
    out = {}
    for p, o in (spec.get("paths") or {}).items():
        for m, op in o.items():
            if m in ("get","post","put","patch","delete") and isinstance(op, dict):
                out[f"{m.upper()} {p}"] = {"id": op.get("operationId"), "dep": bool(op.get("deprecated"))}
    return json.dumps(out, sort_keys=True)
hist = {}
for t, b in tags:
    for k, v in json.loads(ops(b)).items():
        h = hist.setdefault(k, {"ids": set(), "present": [], "dep_first": None})
        h["ids"].add(v["id"]); h["present"].append(t)
        if v["dep"] and not h["dep_first"]: h["dep_first"] = t
all_tags = [t for t, _ in tags]; latest = all_tags[-1]
rows = []
for k, h in hist.items():
    pres = set(h["present"]); first = h["present"][0]; last = h["present"][-1]
    # gaps: tags after first where op absent
    idx = all_tags.index(first)
    gaps = [t for t in all_tags[idx:all_tags.index(last)+1] if t not in pres]
    rows.append({"op": k, "operationIds": sorted(x for x in h["ids"] if x), "introduced": first,
                 "lastSeen": last, "removed": None if last == latest else all_tags[all_tags.index(last)+1],
                 "deprecatedSince": h["dep_first"], "gapTags": len(gaps)})
rows.sort(key=lambda r: (ver(r["introduced"]), r["op"]))
json.dump({"tags": len(all_tags), "latest": latest, "ops": rows}, open("opmatrix.json", "w"), indent=1)
print("tags", len(all_tags), "unique specs", ops.cache_info().currsize, "ops ever", len(rows))
