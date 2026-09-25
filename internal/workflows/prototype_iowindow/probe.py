# PROTOTYPE — probes GET /api/public/v2/observations io/metadata window + limit rules.
import json, sys, base64, urllib.request, urllib.parse, datetime as dt
BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:3000"
AUTH = base64.b64encode(b"pk-lf-proto-local:sk-lf-proto-local").decode()
S = json.load(open("seeded.json")); now = dt.datetime.fromtimestamp(S["now"], dt.timezone.utc) + dt.timedelta(minutes=5)
iso = lambda t: t.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z"
def win(days, extra_s=0): return {"fromStartTime": iso(now - dt.timedelta(days=days, seconds=extra_s)), "toStartTime": iso(now)}
def call(q):
    u = BASE + "/api/public/v2/observations?" + urllib.parse.urlencode(q)
    r = urllib.request.Request(u, headers={"Authorization": "Basic " + AUTH})
    try:
        resp = urllib.request.urlopen(r); d = json.load(resp)
        names = sorted({o.get("name") for o in d["data"] if o.get("name", "").startswith("age-")})
        has_io = any("input" in o for o in d["data"])
        return f"{resp.status} rows={len(d['data'])} io={'y' if has_io else 'n'} ages={','.join(n[4:] for n in names) or '-'} cursor={'y' if d.get('meta',{}).get('cursor') else 'n'}"
    except urllib.error.HTTPError as e:
        return f"{e.code} {e.read().decode()[:300]}"
bulk = S["traces"]["bulk"]
idf = lambda i: json.dumps([{"type": "string", "column": "traceId", "operator": "=", "value": i}])
cases = [
 ("core, no window",                      {"fields": "core", "limit": 1000}),
 ("io, no window",                        {"fields": "core,io", "limit": 10}),
 ("metadata, no window",                  {"fields": "core,metadata", "limit": 10}),
 ("io, 13d",                              {"fields": "core,io", "limit": 10, **win(13)}),
 ("io, 14d exact",                        {"fields": "core,io", "limit": 10, **win(14)}),
 ("io, 14d+1s",                           {"fields": "core,io", "limit": 10, **win(14, 1)}),
 ("io, 15d",                              {"fields": "core,io", "limit": 10, **win(15)}),
 ("io, 30d",                              {"fields": "core,io", "limit": 10, **win(30)}),
 ("metadata, 30d",                        {"fields": "core,metadata", "limit": 10, **win(30)}),
 ("core, 30d",                            {"fields": "core", "limit": 10, **win(30)}),
 ("io, only fromStartTime 30d",           {"fields": "core,io", "limit": 10, "fromStartTime": win(30)["fromStartTime"]}),
 ("io, traceId (d40), no window",         {"fields": "core,io", "traceId": S["traces"]["d40"]}),
 ("io, traceId (d40) + 60d window",       {"fields": "core,io", "traceId": S["traces"]["d40"], **win(60)}),
 ("io, filter traceId (d20), no window",  {"fields": "core,io", "filter": idf(S["traces"]["d20"])}),
 ("io, 13d, limit=50",                    {"fields": "core,io", "limit": 50, **win(13)}),
 ("io, 13d, limit=51",                    {"fields": "core,io", "limit": 51, **win(13)}),
 ("io, 13d, limit=1000",                  {"fields": "core,io", "limit": 1000, **win(13)}),
 ("io, traceId bulk, limit=1000",         {"fields": "core,io", "traceId": bulk, "limit": 1000}),
 ("core, limit=1001",                     {"fields": "core", "limit": 1001}),
 ("core, 13d, limit=1000",                {"fields": "core", "limit": 1000, **win(13)}),
 ("no fields param, 30d",                 {"limit": 10, **win(30)}),
]
for n, q in cases: print(f"{n:40s} {call(q)}")
