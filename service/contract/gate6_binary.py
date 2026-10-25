"""Go packaging/configuration acceptance, using only owned disposable roots."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from harness import ROOT, Service, exact
import harness
from runner import EvidenceResult

BINARY=None

class FreshRelease(Service):
    def start(self):
        # Configuration fixture: Go must create its explicitly selected new root.
        if not hasattr(self,'started_once'):
            self.library.rmdir()
            self.started_once=True
        super().start()

class BinaryPackaging(unittest.TestCase):
    def service(self,command,**extra):
        result=Service(command=command,scenario=self.id(),**extra)
        self.addCleanup(result.close)
        return result

    def test_embedded_assets_are_exact_without_external_web_directory(self):
        s=self.service([BINARY,'--contract-mode'])
        for path,name in [('/','index.html'),('/app.js','app.js'),('/style.css','style.css')]:
            expected=(ROOT/'web'/name).read_bytes()
            if name=='index.html':
                expected=expected.replace(b'__TOKEN__',s.connection['token'].encode())
            code,headers,body=s.request(path)
            self.assertEqual(code,200)
            exact(expected,body)
            self.assertIn('no-store',headers['Cache-Control'])
        self.assertEqual(s.request('/unknown.js')[0],404)
        ident=s.tool('import_item',title='Embedded document',kind='document',
            summary='Embedded summary',original_text='Complete café source',
            source_key='synthetic:embedded')['id']
        s.restart()
        self.assertEqual(s.tool('get_item',item_id=ident)['summary'],'Embedded summary')
        exact('Complete café source'.encode(),s.request('/api/source/'+ident)[2])

    def test_explicit_serve_creates_fresh_configured_library_without_python(self):
        overrides={key:None for key in os.environ if key.startswith('ATLAS_CONTRACT_')}
        overrides['PATH']=''
        s=FreshRelease(command=[BINARY,'--serve','--owner-name','Kai Martínez',
            '--timezone','Pacific/Auckland'],scenario=self.id(),library_name='Fresh Atlas 東京',
            env_overrides=overrides)
        self.addCleanup(s.close)
        token=s.connection['token']
        ident=s.tool('import_item',title='Release document',kind='document',summary='Release summary',
            original_text='Full release source',source_key='synthetic:release')['id']
        item=s.tool('get_item',item_id=ident)
        self.assertEqual(item['date_basis'],'added')
        self.assertEqual(s.get('/health')['library'],str(s.library))
        self.assertEqual(s.request('/api/source/'+ident)[2],b'Full release source')
        s.restart()
        self.assertEqual(s.connection['token'],token)
        exact(item,s.tool('get_item',item_id=ident))
        self.assertEqual(s.request('/health',headers={'Authorization':'Bearer wrong'})[0],403)

    def test_invalid_modes_configuration_and_contract_env_reject_before_writes(self):
        with tempfile.TemporaryDirectory(prefix='atlas-contract-') as scratch:
            parent=Path(scratch).resolve()
            (parent/'.atlas-contract-fixture').write_text('atlas-contract-v1')
            common=['--owner-name','Kai Example','--timezone','UTC','--port','0']
            cases=[
                ([],common,{}),
                (['--serve','--contract-mode'],common,{}),
                (['--serve'],['--owner-name',' ','--timezone','UTC'],{}),
                (['--serve'],['--owner-name','Kai Example'],{}),
                (['--serve'],['--owner-name','Kai Example','--timezone','Not/AZone'],{}),
                (['--serve'],common+['--port','65536'],{}),
                (['--serve'],common,{'ATLAS_CONTRACT_FAULT':''}),
                (['--serve'],common,{'ATLAS_CONTRACT_UNKNOWN':''}),
                (['--serve'],common+['--web-root',str(parent/'missing-web')],{}),
            ]
            for index,(modes,flags,extra) in enumerate(cases):
                with self.subTest(case=index):
                    library=parent/('rejected-'+str(index))
                    env={key:value for key,value in os.environ.items() if not key.startswith('ATLAS_CONTRACT_')}
                    env.update(extra)
                    result=subprocess.run([BINARY]+modes+flags+['--library',str(library)],
                        env=env,capture_output=True,timeout=5)
                    self.assertNotEqual(result.returncode,0)
                    self.assertFalse(library.exists(),'Invalid configuration created library files')
            env={key:value for key,value in os.environ.items() if not key.startswith('ATLAS_CONTRACT_')}
            result=subprocess.run([BINARY,'--serve']+common+['--library','relative-rejected'],
                env=env,cwd=parent,capture_output=True,timeout=5)
            self.assertNotEqual(result.returncode,0)
            self.assertFalse((parent/'relative-rejected').exists())
            unmarked=parent/'unmarked';unmarked.mkdir()
            (parent/'.atlas-contract-fixture').unlink()
            result=subprocess.run([BINARY,'--contract-mode','--library',str(unmarked)],
                env=env,capture_output=True,timeout=5)
            self.assertNotEqual(result.returncode,0)
            self.assertEqual(list(unmarked.iterdir()),[])

def main():
    global BINARY
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary',required=True)
    args=parser.parse_args()
    BINARY=str(Path(args.binary).resolve())
    result=unittest.TextTestRunner(verbosity=2,resultclass=EvidenceResult).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(BinaryPackaging))
    passed=result.testsRun>0 and result.wasSuccessful() and not result.skipped and all(r['status']=='pass' for r in result.outcomes)
    stamp=dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    path=ROOT/'service/.work/reports'/('gate6-binary-'+stamp+'.json')
    path.parent.mkdir(parents=True,exist_ok=True)
    path.write_text(json.dumps(dict(passed=passed,tests_run=result.testsRun,test_outcomes=result.outcomes,
        observed_requests=harness.OBSERVED),indent=2)+'\n')
    print('Binary packaging report:',path)
    return 0 if passed else 1

if __name__=='__main__':
    sys.exit(main())
