"""Lossy input must never become a successful preserved-source import."""
import hashlib
import json
from harness import EndpointCase, exact


class InputUnicodeContracts(EndpointCase):
    def rpc_bytes(self, payload):
        return json.dumps(payload,ensure_ascii=True).encode("ascii")

    def test_unpaired_surrogates_fail_without_import_and_valid_pairs_remain_exact(self):
        # A malformed request may fail at the MCP tool or JSON decoding boundary; reject malformed
        # Unicode at HTTP parsing (400). This reviewed deviation prevents lossy
        # replacement decoding. Neither response may claim an import succeeded.
        for index,text in enumerate((chr(0xd800),chr(0xdc00),chr(0xd800)+"A",
                                     chr(0xd800)+chr(0xd800))):
            payload={"jsonrpc":"2.0","id":index,"method":"tools/call",
                "params":{"name":"import_item","arguments":{
                    "title":"Invalid Unicode","summary":"Summary","kind":"document",
                    "date":"2026-01-01","source_key":"unicode:invalid:"+str(index),
                    "original_text":text}}}
            status,_,body=self.request("/mcp",self.rpc_bytes(payload))
            self.assertIn(status,(200,400))
            response=json.loads(body)
            if status==200:
                exact(True,response["result"]["isError"])
            else:
                self.assertIn("error",response)
            exact(0,self.get("/api/info")["count"])
            exact([],self.tool("list_activity")["events"])
        for index,text in enumerate((chr(0x1f600),chr(0xfffd))):
            payload={"jsonrpc":"2.0","id":"valid-"+str(index),"method":"tools/call",
                "params":{"name":"import_item","arguments":{
                    "title":"Valid Unicode "+str(index),"summary":"Summary","kind":"document",
                    "date":"2026-01-01","source_key":"unicode:valid:"+str(index),
                    "original_text":text}}}
            status,_,body=self.request("/mcp",self.rpc_bytes(payload))
            exact(200,status)
            response=json.loads(body)
            self.assertFalse(response["result"].get("isError"),response)
            ident=json.loads(response["result"]["content"][0]["text"])["id"]
            item=self.get("/api/item/"+ident)
            exact(hashlib.sha256(text.encode("utf-8")).hexdigest(),item["source_hash"])
            status,_,body=self.request("/api/source/"+ident)
            exact(200,status)
            exact(text.encode("utf-8"),body)
        exact(2,self.get("/api/info")["count"])
        exact(2,len(self.tool("list_activity")["events"]))
