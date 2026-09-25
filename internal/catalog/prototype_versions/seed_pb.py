# PROTOTYPE — same dataset as seed.py, sent as OTLP/HTTP protobuf (Langfuse 3.x mangles OTLP/JSON ids and timestamps).
import json, time, base64, urllib.request, secrets, sys
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import ExportTraceServiceRequest
from opentelemetry.proto.trace.v1.trace_pb2 import Span
from opentelemetry.proto.common.v1.common_pb2 import KeyValue, AnyValue
BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:3000"
AUTH = base64.b64encode(b"pk-lf-proto-local:sk-lf-proto-local").decode()
now = time.time(); DAY = 86400
req = ExportTraceServiceRequest(); rs = req.resource_spans.add()
rs.resource.attributes.append(KeyValue(key="service.name", value=AnyValue(string_value="proto-seed")))
ss = rs.scope_spans.add(); ss.scope.name = "proto"
def span(tid, name, age, parent=None):
    s = ss.spans.add(); s.trace_id = tid; s.span_id = secrets.token_bytes(8); s.name = name; s.kind = Span.SPAN_KIND_INTERNAL
    st = int((now - age) * 1e9); s.start_time_unix_nano = st; s.end_time_unix_nano = st + 500_000_000
    if parent: s.parent_span_id = parent
    for k, v in [("langfuse.observation.input", json.dumps({"q": name})), ("langfuse.observation.output", json.dumps({"a": "out-" + name})), ("langfuse.observation.metadata.proto", "age-%ds" % age)]:
        s.attributes.append(KeyValue(key=k, value=AnyValue(string_value=v)))
    return s.span_id
traces = {}
for label, age in [("d1", 1*DAY), ("d13", 13*DAY), ("d14_5", 14.5*DAY), ("d16", 16*DAY), ("d20", 20*DAY), ("d40", 40*DAY)]:
    t = secrets.token_bytes(16); span(t, "age-" + label, age); traces[label] = t.hex()
t = secrets.token_bytes(16); rid = span(t, "bulk-root", 2*DAY); traces["bulk"] = t.hex()
for i in range(60): span(t, "bulk-%02d" % i, 2*DAY - i, rid)
r = urllib.request.urlopen(urllib.request.Request(BASE + "/api/public/otel/v1/traces", data=req.SerializeToString(), headers={"Authorization": "Basic " + AUTH, "Content-Type": "application/x-protobuf"}))
print("otel protobuf", r.status)
json.dump({"now": now, "traces": traces}, open("seeded.json", "w")); print(traces)
