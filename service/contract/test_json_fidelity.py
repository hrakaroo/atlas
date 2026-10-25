"""Endpoint regressions for JSON text fidelity and exact integer metadata."""
import hashlib
import json
from harness import EndpointCase, exact


class JSONFidelityContracts(EndpointCase):
    def test_literal_unicode_escapes_separators_large_integers_and_zero_flags_roundtrip(self):
        literal=chr(92)+"u2028 "+chr(92)+"u2029"
        actual=chr(0x2028)+chr(0x2029)
        spellings=[chr(92)*count+"u2028" for count in (1,2,3)]
        spellings += [chr(92)*count+actual for count in (1,2,3)]
        metadata={"literal":literal,"actual":actual,"integer":9007199254740993,
                  "negative":-9007199254740993,"spellings":spellings,
                  "nested":{"integer":9007199254740993,"null":None,"boolean":False}}
        title="JSON fidelity "+literal+actual
        summary="Summary "+literal+actual
        source="Original "+literal+actual+"\r\n"
        # metadata is a legacy accepted dispatch argument, although not advertised
        # in the MCP schema. The same endpoint compatibility path exists in Python.
        ident=self.tool("import_item",title=title,summary=summary,kind="document",
            date="2026-01-01",original_text=source,original_name="literal.txt",
            source_key="json:literal",metadata=metadata)["id"]
        def check():
            item=self.get("/api/item/"+ident)
            exact(title,item["title"])
            exact(summary,item["summary"])
            exact(hashlib.sha256(source.encode()).hexdigest(),item["source_hash"])
            for key,value in metadata.items():
                exact(value,item["metadata"][key])
            status,_,body=self.request("/api/source/"+ident)
            exact(200,status)
            exact(source.encode(),body)
            return item
        before=check()
        peer=self.tool("import_item",title="Starred peer",summary="Summary",
            kind="document",date="2026-01-01",original_text="Peer source",
            source_key="json:peer")["id"]
        self.tool("set_starred",item_id=peer,starred=True)
        # Exercise numeric lexical forms, including exponent zero, directly in
        # a well-formed JSON-RPC request without converting them in the runner.
        for zero in ("0","-0","0.0","-0.0","0e0","0.000E+2"):
            payload={"jsonrpc":"2.0","id":"zero-"+zero,"method":"tools/call",
                     "params":{"name":"search_items","arguments":{"starred_only":"ZERO_TOKEN"}}}
            encoded=json.dumps(payload).replace('"ZERO_TOKEN"',zero).encode()
            status,_,body=self.request("/mcp",encoded)
            exact(200,status)
            response=json.loads(body)
            exact("zero-"+zero,response["id"])
            self.assertNotIn("error",response)
            self.assertFalse(response["result"].get("isError"),response)
            rows=json.loads(response["result"]["content"][0]["text"])
            exact({ident,peer},{row["id"] for row in rows})
        self.restart()
        exact(before,check())
