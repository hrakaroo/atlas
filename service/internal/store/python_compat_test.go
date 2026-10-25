//go:build atlas_python_reference

// Optional historical differential check; requires the ignored local old/ archive.
package store

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

const pythonEndpointFixture = `import http.client,json,subprocess,sys,time
from pathlib import Path
from urllib.parse import urlsplit
server,root,mode=sys.argv[1:4]
process=subprocess.Popen([sys.executable,server,"--library",root,"--port","0"],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
def request(path,payload=None):
 endpoint=urlsplit(settings["url"])
 connection=http.client.HTTPConnection("127.0.0.1",endpoint.port,timeout=5)
 headers={"Authorization":"Bearer "+settings["token"],"Accept":"application/json, text/event-stream","Content-Type":"application/json"}
 body=json.dumps(payload,ensure_ascii=False).encode() if payload is not None else None
 try:
  connection.request("POST" if payload is not None else "GET",path,body,headers)
  response=connection.getresponse()
  result=response.read()
  assert response.status==200,(path,response.status)
  return result
 finally: connection.close()
def get(path): return json.loads(request(path))
def tool(name,**arguments):
 result=json.loads(request("/mcp",{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":name,"arguments":arguments}}))["result"]
 assert not result.get("isError"),result
 return json.loads(result["content"][0]["text"])
try:
 deadline=time.monotonic()+10
 while time.monotonic()<deadline:
  assert process.poll() is None,"Python fixture service exited"
  try:
   settings=json.loads((Path(root)/".service/connection.json").read_text())
   if get("/health")["pid"]==process.pid: break
  except (OSError,ValueError,KeyError,http.client.HTTPException): pass
  time.sleep(0.025)
 else: raise AssertionError("Python fixture service readiness failed")
 if mode=="create":
  item=tool("import_item",title="Endpoint fixture",summary="Original summary",kind="document",date="2026-10-01",source_key="fixture:doc",original_text="Complete café\r\n東京 transcript",doc_urls=["https://docs.google.com/document/d/fixture-source/edit"],tags=[{"name":"Fixture Team","category":"team"}])
  ident=item["id"]
  tool("set_starred",item_id=ident,starred=True)
  revision=tool("get_item",item_id=ident)["revision"]
  tool("update_summary",item_id=ident,expected_revision=revision,summary="Updated summary")
  removed=tool("import_item",title="Removed fixture",summary="Removed",kind="document",date="2026-10-01",source_key="fixture:removed",original_text="Removed complete source")["id"]
  tool("delete_item",item_id=removed,expected_revision=tool("get_item",item_id=removed)["revision"])
  tool("submit_sources",sources=[{"type":"zoom","source_key":"zoom-transcript:2026-10-02 10.00.00 Fixture/transcript.txt","relative_path":"2026-10-02 10.00.00 Fixture/transcript.txt","original_name":"transcript.txt","original_text":"Candidate: Full queued transcript","title":"Fixture meeting","date":"2026-10-02"}])
 else: ident=sys.argv[4]
 snapshot={"id":ident,"item":tool("get_item",item_id=ident),"items":get("/api/items"),"source":request("/api/source/"+ident).decode(),"history":get("/api/history/"+ident),"trash":tool("list_trash"),"intake":tool("list_intake"),"activity":tool("list_activity"),"entities":tool("list_entities")}
 print(json.dumps(snapshot,ensure_ascii=False,sort_keys=True,separators=(",",":")))
finally:
 process.terminate()
 try: process.wait(timeout=5)
 except subprocess.TimeoutExpired:
  process.kill();process.wait(timeout=5)
 process.stderr.close()
`

func TestPythonEndpointFixtureSurvivesGoOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "library")
	server := filepath.Join(projectRoot(), "old", "app", "server.py")
	run := func(mode, id string) []byte {
		command := exec.Command("/usr/bin/python3", "-c", pythonEndpointFixture, server, root, mode, id)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Python endpoint fixture %s: %v: %s", mode, err, out)
		}
		return out
	}
	before := run("create", "")
	var identity struct {
		ID string `json:"id"`
	}
	must(t, json.Unmarshal(before, &identity))
	if identity.ID == "" {
		t.Fatal("Missing endpoint fixture identity")
	}
	s, err := Open(root)
	must(t, err)
	pragmas(t, s)
	must(t, s.Close())
	after := run("verify", identity.ID)
	if string(before) != string(after) {
		t.Fatal("Python endpoint-visible source, history, organization, intake, Trash or Activity changed after Go schema open")
	}
}
