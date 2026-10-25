"""Signed native supervisor smoke on one owned, marked disposable library.

Does not install/register apps or invoke launchd. Normal Service PID checks stay
unchanged: this explicit orchestrator validates the backend PID's parent, then
verifies supervisor TERM releases service ownership before direct reopen.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

import harness
from harness import Service, exact


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--app',type=Path,required=True)
    parser.add_argument('--go-service',type=Path,required=True)
    parser.add_argument('--backend',choices=['go'],default='go')
    args=parser.parse_args()
    app=args.app.resolve()
    helper=app/'Contents/Library/LoginItems/AtlasService.app/Contents/MacOS/AtlasService'
    go=args.go_service.resolve()
    if not app.is_absolute() or not helper.is_file() or not go.is_file():
        parser.error('Supply an existing signed app and native Go executable')
    direct=[str(go),'--contract-mode','--owner-name','Kai Martínez','--timezone','Pacific/Auckland']
    service=Service(command=direct,library_name='Signed helper 東京 library',scenario='gate6_native_supervisor')
    child=None
    log=None
    try:
        # Fixture activation marker prohibits the real client compatibility path
        # from creating an independent owner after a deliberate service outage.
        (service.library/'.service/enabled').touch()
        service.stop()
        # Conflicting/relative backend selections must fail before spawning an
        # owner; the later successful start also proves no lock was retained.
        base=[str(helper),'--service','--library',str(service.library),'--port','0']
        invalid=[base+['--go-service',str(go),'--python',sys.executable,
                       '--server',str(harness.ROOT/'retired-server.py'),'--owner-name','Kai Martínez','--timezone','UTC'],
                 base+['--go-service','relative-native','--owner-name','Kai Martínez','--timezone','UTC']]
        invalid += [base+['--go-service',str(go),'--owner-name','Kai Martínez','--timezone','UTC','--python'],
                    base+['--go-service',str(go),'--owner-name','   ','--timezone','UTC']]
        for rejected in invalid:
            result=subprocess.run(rejected,stdin=subprocess.DEVNULL,capture_output=True,timeout=10)
            assert result.returncode!=0,'Invalid native backend selection was accepted'
        command=[str(helper),'--service','--library',str(service.library),'--port','0']
        command+=['--go-service',str(go),'--owner-name','Kai Martínez','--timezone','Pacific/Auckland','--contract-mode']
        log=open(service.library.parent/'native-helper.log','ab')
        # Exercise the reviewed launch specification's working directory too;
        # Go must not depend on the source checkout to start.
        working_directory=service.library.parent
        if args.backend=='go':
            sys.path.insert(0,str(harness.ROOT/'scripts'))
            from service import launch_spec
            spec=launch_spec(service.library,47831,'go',app,'Kai Martínez','Pacific/Auckland')
            exact(str(service.library.resolve()),spec['WorkingDirectory'])
            working_directory=spec['WorkingDirectory']
        child=subprocess.Popen(command,stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT,
                               cwd=working_directory,env={**os.environ,'PATH':''})
        deadline=time.monotonic()+15
        health=None
        while time.monotonic()<deadline:
            if child.poll() is not None:
                raise AssertionError('Native helper exited before readiness')
            try:
                settings=json.loads((service.library/'.service/connection.json').read_text())
                from urllib.parse import urlsplit
                location=urlsplit(settings['url'])
                assert settings['library']==str(service.library.resolve())
                assert location.scheme=='http' and location.hostname=='127.0.0.1' and location.port
                assert not location.path and not location.query and not location.fragment
                assert not location.username and not location.password and settings['token']
                service.connection=settings
                status,_,body=service.request('/health')
                candidate=json.loads(body)
                if status==200 and candidate['pid']!=service.pid:
                    health=candidate
                    break
            except (OSError,ValueError,KeyError,ConnectionError):
                pass
            time.sleep(.05)
        assert health is not None,'Supervised backend did not become ready'
        assert health['pid']!=child.pid
        parent=subprocess.run(['/bin/ps','-p',str(health['pid']),'-o','ppid='],capture_output=True,text=True,check=True)
        exact(child.pid,int(parent.stdout.strip()))
        exact(str(service.library.resolve()),health['library'])
        # Exercise the actual retained HTTP client, not a new Library owner.
        sys.path.insert(0,str(harness.ROOT/'app'))
        from service_client import call_tool
        imported=call_tool('import_item',{'title':'Native helper source','kind':'document',
            'summary':'Signed helper summary','source_key':'gate6:native',
            'original_text':'Complete helper source — 東京\r\n'},service.library)
        ident=imported['id']
        record=call_tool('get_item',{'item_id':ident},service.library)
        exact('Signed helper summary',record['summary'])
        status,_,body=service.request('/api/source/'+ident)
        exact(200,status);exact('Complete helper source — 東京\r\n'.encode(),body)
        child.terminate()
        child.wait(timeout=10)
        assert child.returncode==0,'Supervisor failed graceful termination'
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            present=subprocess.run(['/bin/ps','-p',str(health['pid']),'-o','pid='],capture_output=True,text=True)
            if not present.stdout.strip(): break
            time.sleep(.05)
        else: raise AssertionError('Supervised child remains alive after helper TERM')
        try:
            call_tool('get_item',{'item_id':ident},service.library)
        except ValueError:
            pass
        else: raise AssertionError('Activated client unexpectedly survived the deliberate service outage')
        # Actual reopen proves both service and writer locks are released.
        service.restart(command=direct)
        exact(record,service.tool('get_item',item_id=ident))
        service.restart(command=direct)
        exact(record,service.tool('get_item',item_id=ident))
        report={'scope':'Isolated signed helper supervision/client roundtrip; no registration, launchd or privacy-consent claim',
                'backend':args.backend,'app':str(app),'passed':True,
                'assertions':['conflicting/malformed backend flags, relative executable and blank owner rejected','backend PID is immediate child','retained HTTP client roundtrip','complete source bytes',
                              'TERM exits supervisor and child','activated outage fails closed',
                              'Go reopens the same fixture after locks release']}
        folder=harness.ROOT/'service/.work/reports';folder.mkdir(parents=True,exist_ok=True)
        path=folder/('gate6-helper-'+args.backend+'-'+str(time.time_ns())+'.json')
        path.write_text(json.dumps(report,indent=2)+'\n')
        print('Native helper endpoint report:',path)
    finally:
        if child is not None and child.poll() is None:
            child.terminate()
            try: child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                child.kill();child.wait(timeout=5)
        if log is not None: log.close()
        service.close()


if __name__=='__main__': main()
