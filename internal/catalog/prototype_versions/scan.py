# PROTOTYPE — classify every spec operation on a throwaway deployment: present / missing (HTML 404) / events_only.
import json,base64,urllib.request,re,sys
spec=json.load(open(sys.argv[1])); base=sys.argv[2]
A=base64.b64encode(b"pk-lf-proto-local:sk-lf-proto-local").decode()
out=[]
for path,ops in spec["paths"].items():
  for m,op in ops.items():
    if m not in ("get","post","put","patch","delete"): continue
    u=re.sub(r"\{[^}]+\}","proto-nonexistent",path)
    data=None if m=="get" else b"{}"
    req=urllib.request.Request(base+u,data=data,method=m.upper(),headers={"Authorization":"Basic "+A,"Content-Type":"application/json"})
    try: r=urllib.request.urlopen(req,timeout=20); st=r.status; b=r.read(300).decode("utf-8","replace")
    except urllib.error.HTTPError as e: st=e.code; b=e.read(300).decode("utf-8","replace")
    except Exception as e: st="ERR"; b=str(e)
    cls="MISSING(html404)" if b.lstrip().startswith("<!DOCTYPE") or b.lstrip().startswith("<html") else "events_only" if "events_only" in b else "present"
    out.append({"op":op.get("operationId"),"method":m.upper(),"path":path,"deprecated":op.get("deprecated",False),"status":st,"class":cls,"body":b[:160]})
json.dump(out,open(sys.argv[3],"w"),indent=1)
from collections import Counter
print(Counter(o["class"] for o in out))
for o in out:
  if o["class"]!="present": print(o["class"],o["method"],o["op"],o["path"],"dep" if o["deprecated"] else "")
