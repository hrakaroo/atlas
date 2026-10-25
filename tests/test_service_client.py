"""HTTP clients must never construct an archived backend during an outage."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'app'))
import service_client

class ClientTests(unittest.TestCase):
    def test_outage_without_activation_marker_never_opens_local_library(self):
        with tempfile.TemporaryDirectory() as folder:
            with patch.object(service_client,'request',side_effect=ValueError('Unavailable')):
                with patch.dict(sys.modules,{'library':None,'server':None}):
                    with self.assertRaisesRegex(ValueError,'Unavailable'):
                        service_client.call_tool('search_items',{},Path(folder)/'library')
            self.assertFalse((Path(folder)/'library').exists())

    def test_tool_response_remains_decoded_from_http(self):
        result={'result':{'content':[{'type':'text','text':json.dumps({'id':'example','revision':3})}]}}
        with patch.object(service_client,'request',return_value=result) as request:
            self.assertEqual(service_client.call_tool('get_item',{'item_id':'example'}),{'id':'example','revision':3})
            self.assertEqual(request.call_args[0][1],'/mcp')
