# PROTOTYPE — io/window/limit behavior of the legacy (v1) read routes on self-hosted v3.
import json,sys,base64,urllib.request,urllib.parse,datetime as dt
B="http://localhost:3000/api/public"; A=base64.b64encode(b"pk-lf-proto-local:sk-lf-proto-local").decode()
S=json.load(open("seeded.json")); now=dt.datetime.now(dt.timezone.utc)+dt.timedelta(minutes=5)
iso=lambda t:t.strftime("%Y-%m-%dT%H:%M:%SZ"); ago=lambda d:iso(now-dt.timedelta(days=d))
def call(p,q):
    try:
        r=urllib.request.urlopen(urllib.request.Request(B+p+"?"+urllib.parse.urlencode(q),headers={"Authorization":"Basic "+A})); d=json.load(r)
        rows=d.get("data",d if isinstance(d,list) else []); m=d.get("meta",{})
        io=any(o.get("input") for o in rows) if isinstance(rows,list) else "?"
        return f"200 rows={len(rows) if isinstance(rows,list) else '?'} io={io} total={m.get('totalItems')}"
    except urllib.error.HTTPError as e: return f"{e.code} {e.read()[:250].decode()}"
C=[("/observations","v1 obs no window limit=10",{"limit":10}),
   ("/observations","v1 obs 30d window",{"fromStartTime":ago(30),"toStartTime":iso(now),"limit":100}),
   ("/observations","v1 obs limit=100",{"limit":100}),
   ("/observations","v1 obs limit=101",{"limit":101}),
   ("/observations","v1 obs limit=1000",{"limit":1000}),
   ("/observations","v1 obs limit=1001",{"limit":1001}),
   ("/observations","v1 obs traceId d40",{"traceId":S["traces"]["d40"]}),
   ("/traces","traces no window",{"limit":100}),
   ("/traces","traces fields=io 30d",{"fields":"core,io","fromTimestamp":ago(30),"toTimestamp":iso(now),"limit":100}),
   ("/traces","traces limit=101",{"limit":101}),
   ("/observations","v1 obs 13d window",{"fromStartTime":ago(13),"toStartTime":iso(now),"limit":100}),
   ("/observations","v1 obs 15d window",{"fromStartTime":ago(15),"toStartTime":iso(now),"limit":100}),
   ("/traces","traces 60d window",{"fromTimestamp":ago(60),"toTimestamp":iso(now),"limit":100}),
   ("/traces","traces limit=1001",{"limit":1001}),
   ("/traces/"+S["traces"]["bulk"],"trace get bulk (io+obs)",{}),
   ("/sessions","sessions",{"limit":10}),
   ("/v2/scores","scores v2",{"limit":10}),
   ("/metrics","metrics v1 traces count 60d",{"query":json.dumps({"view":"traces","metrics":[{"measure":"count","aggregation":"count"}],"dimensions":[],"filters":[],"fromTimestamp":ago(60),"toTimestamp":iso(now)})}),
]
for p,n,q in C: print(f"{n:32s} {call(p,q)}")
g=json.load(urllib.request.urlopen(urllib.request.Request(B+"/traces/"+S["traces"]["bulk"],headers={"Authorization":"Basic "+A})))
print("trace get bulk: observations embedded =",len(g.get("observations",[])),"| has input:",bool(g.get("input")) or any(o.get("input") for o in g.get("observations",[])))
