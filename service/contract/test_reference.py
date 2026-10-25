"""Explicit canonicalization must never erase semantic changes."""
import json
import os
import subprocess
import sys
from pathlib import Path
import unittest
from harness import exact
from reference import Canonical, capture, HERE


class ReferenceContracts(unittest.TestCase):
    def test_fixed_clock_reference_matches_frozen_endpoint_baseline(self):
        expected=json.loads((HERE/'fixtures/python-reference.json').read_text())
        first=capture()
        exact(expected,first)
        exact(first,capture())

    def test_baseline_writer_rejects_inherited_backend_override(self):
        environment=dict(os.environ,ATLAS_CONTRACT_COMMAND='["not-the-python-reference"]')
        result=subprocess.run([sys.executable,str(HERE/'reference.py'),'--write-baseline'],
                              env=environment,capture_output=True,text=True,timeout=3)
        self.assertEqual(result.returncode,2)
        self.assertIn('forbids command or inherited environment overrides',result.stderr)

    def test_canonicalizer_preserves_hashes_content_types_counts_and_unknown_fields(self):
        c=Canonical(None)
        c.bind('generated-id','item:fixture')
        c.paths['/fixture/source.txt']='<LIB>/source.txt'
        self.assertEqual(c.value({'id':'generated-id','source_path':'/fixture/source.txt'}),
                         {'id':'item:fixture','source_path':'<LIB>/source.txt'})
        original={'revision':2,'summary':'generated-id','source_hash':'0'*64,
                  'source_bytes':'00ff','events':[{'item_id':'generated-id'}],
                  'extra':None,'starred':True}
        baseline=c.value(original)
        self.assertEqual(baseline['summary'],'generated-id')
        self.assertEqual(baseline['source_hash'],'0'*64)
        for update in [{'revision':3},{'summary':'other'},{'source_hash':'1'*64},
                       {'source_bytes':'00fe'},{'events':[]},{'starred':1}]:
            with self.assertRaises(AssertionError):
                exact(baseline,c.value(dict(original,**update)))
        with self.assertRaises(AssertionError):
            c.value({'source_path':'/unreviewed/path'})
        with self.assertRaises(AssertionError):
            c.bind('different-id','item:fixture')
        with self.assertRaises(AssertionError):
            c.bind('generated-id','item:other')
