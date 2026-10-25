"""Gate 2 operational orchestration with endpoint assertions on disposable libraries."""
import argparse
import json
import os
from unittest.mock import patch
import subprocess
import sys
from harness import Service, ROOT, exact


def exercise(command, peer):
    service=Service(command=command,scenario='foundation-ownership')
    try:
        exact(0,service.get('/api/info')['count'])
        original=service.connection.copy()
        pid=service.get('/health')['pid']
        duplicate=subprocess.run(command+['--library',str(service.library),'--port','0'],
            capture_output=True,timeout=5)
        if duplicate.returncode==0:
            raise AssertionError('Duplicate service unexpectedly started')
        exact(pid,service.get('/health')['pid'])
        # Protected connection settings are the approved operational exception.
        exact(original,json.loads((service.library/'.service/connection.json').read_text()))
        duplicate=subprocess.run(peer+['--library',str(service.library),'--port','0'],
            capture_output=True,timeout=5)
        if duplicate.returncode==0:
            raise AssertionError('Cross-engine duplicate unexpectedly started')
        exact(original,json.loads((service.library/'.service/connection.json').read_text()))
        exact(pid,service.get('/health')['pid'])
        service.stop()
        service.command=peer
        service.start()
        exact(0,service.get('/api/info')['count'])
        ident=service.tool('import_item',title='Schema handoff fixture',kind='document',
            summary='Preserved summary',original_text='Preserved café\r\n東京',
            original_name='fixture.txt',source_key='foundation:handoff')['id']
        expected=service.get('/api/item/'+ident)
        source=service.request('/api/source/'+ident)[2]
        service.stop()
        service.command=command
        with patch.dict(os.environ,{'PATH':''}):
            service.start()
        exact(1,service.get('/api/info')['count'])
        service.stop()
        service.command=peer
        service.start()
        exact(expected,service.get('/api/item/'+ident))
        exact(source,service.request('/api/source/'+ident)[2])
        print('PASS: Go restart without PATH executables, same/cross-engine ownership exclusion, schema reopen and Python endpoint readback.')
    finally:
        service.close()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--command',required=True,help='JSON argv of the compiled Go foundation')
    args=parser.parse_args()
    command=json.loads(args.command)
    if not isinstance(command,list) or not command or not all(isinstance(value,str) for value in command):
        parser.error('command must be a nonempty JSON argv')
    peer=[sys.executable,str(ROOT/'old/contract/python_backend.py')]
    exercise(command,peer)


if __name__=='__main__':
    main()
