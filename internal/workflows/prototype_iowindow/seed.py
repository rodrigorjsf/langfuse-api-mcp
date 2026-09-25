# PROTOTYPE — seeds back-dated observations via OTLP/HTTP JSON into a throwaway Langfuse.
import json, os, time, base64, urllib.request, secrets
BASE = "http://localhost:3000"; AUTH = base64.b64encode(b"pk-lf-proto-local:sk-lf-proto-local").decode()
now = time.time(); DAY = 86400
def span(trace, name, age_s, parent=None):
    st = int((now - age_s) * 1e9); sid = secrets.token_hex(8)
    a = [{"key": "langfuse.observation.input", "value": {"stringValue": json.dumps({"q": name})}},
         {"key": "langfuse.observation.output", "value": {"stringValue": json.dumps({"a": "out-" + name})}},
         {"key": "langfuse.observation.metadata.proto", "value": {"stringValue": "age-%ds" % age_s}}]
    s = {"traceId": trace, "spanId": sid, "name": name, "kind": 1, "startTimeUnixNano": str(st),
         "endTimeUnixNano": str(st + 500_000_000), "attributes": a}
    if parent: s["parentSpanId"] = parent
    return s, sid
spans, traces = [], {}
for label, age in [("d1", 1*DAY), ("d13", 13*DAY), ("d14_5", 14.5*DAY), ("d16", 16*DAY), ("d20", 20*DAY), ("d40", 40*DAY)]:
    t = secrets.token_hex(16); s, _ = span(t, "age-" + label, age); spans.append(s); traces[label] = t
t = secrets.token_hex(16); root, rid = span(t, "bulk-root", 2*DAY); spans.append(root); traces["bulk"] = t
for i in range(60):
    s, _ = span(t, "bulk-%02d" % i, 2*DAY - i, rid); spans.append(s)
body = {"resourceSpans": [{"resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "proto-seed"}}]},
        "scopeSpans": [{"scope": {"name": "proto"}, "spans": spans}]}]}
req = urllib.request.Request(BASE + "/api/public/otel/v1/traces", data=json.dumps(body).encode(), method="POST",
      headers={"Authorization": "Basic " + AUTH, "Content-Type": "application/json"})
r = urllib.request.urlopen(req); print("otel", r.status, r.read()[:200])
json.dump({"now": now, "traces": traces}, open("seeded.json", "w")); print(json.dumps(traces, indent=1))
