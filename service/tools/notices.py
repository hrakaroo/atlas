"""Reproduce third-party notices from the pinned local Go module cache, offline."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

ROOT=Path(__file__).resolve().parents[1]
PREFIXES=('LICENSE','COPYING','NOTICE','COPYRIGHT','PATENTS')


def go(*args):
    environment={**os.environ,'GOPROXY':'off','GOSUMDB':'off','GOTOOLCHAIN':'local'}
    return subprocess.run(['go',*args],cwd=ROOT,env=environment,check=True,capture_output=True).stdout


def objects(data):
    text=data.decode('utf-8');decoder=json.JSONDecoder();offset=0
    while offset<len(text):
        while offset<len(text) and text[offset].isspace(): offset+=1
        if offset==len(text): break
        value,offset=decoder.raw_decode(text,offset)
        yield value


def notice_files(root):
    files=[]
    for path in sorted(root.iterdir()):
        if any(path.name.upper().startswith(prefix) for prefix in PREFIXES):
            if path.is_file(): files.append(path)
            elif path.is_dir(): files.extend(sorted(p for p in path.rglob('*') if p.is_file()))
    if not files:
        raise ValueError('Required module notice missing')
    if not any(path.name.upper().startswith(('LICENSE','COPYING')) for path in files):
        raise ValueError('Required module license missing')
    return files


def generate():
    modules=sorted((m for m in objects(go('list','-m','-json','all')) if not m.get('Main')),key=lambda m:m['Path'])
    toolchain=json.loads(go('env','-json','GOROOT','GOVERSION'))
    goroot=Path(toolchain['GOROOT'])
    go_license=goroot/'LICENSE'
    # Homebrew moves the distribution license above libexec/GOROOT. Read that
    # actual local file; never synthesize text or accept PATENTS as a license.
    if not go_license.is_file() and goroot.name=='libexec':
        go_license=goroot.parent/'LICENSE'
    if not go_license.is_file():
        raise ValueError('Go toolchain LICENSE is unavailable in GOROOT or its Homebrew distribution root')
    toolchain_files=[go_license]+([goroot/'PATENTS'] if (goroot/'PATENTS').is_file() else [])
    roots=[('Go toolchain',toolchain['GOVERSION'],goroot,None)]
    for module in modules:
        if module.get('Replace') or not module.get('Dir') or not module.get('Version'):
            raise ValueError('Every dependency must have its pinned unmodified module cached: '+module['Path'])
        roots.append((module['Path'],module['Version'],Path(module['Dir']),module.get('Sum')))
    output=bytearray(b'Atlas native Go preview: third-party notices\n\n'
        b'Scope: Go toolchain root notices and every module in the pinned Go build graph,\n'
        b'conservatively including build/test-only modules and optional module features.\n'
        b'Each source notice below is copied byte-for-byte without legal reinterpretation.\n'
        b'This list does not claim every listed module or optional feature is linked.\n\n')
    manifest={'scope':'Go toolchain root notices plus all pinned module-graph dependencies',
              'go_mod_sha256':hashlib.sha256((ROOT/'go.mod').read_bytes()).hexdigest(),'components':[]}
    for name,version,folder,checksum in roots:
        component={'module':name,'version':version,'module_sum':checksum,'notices':[]}
        files=toolchain_files if name=='Go toolchain' else notice_files(folder)
        for path in files:
            content=path.read_bytes();content.decode('utf-8')
            relative=path.name if name=='Go toolchain' else path.relative_to(folder).as_posix()
            digest=hashlib.sha256(content).hexdigest()
            component['notices'].append({'file':relative,'sha256':digest,'bytes':len(content)})
            output.extend(('===== '+name+' '+version+' / '+relative+' =====\n'
                           +'SHA-256: '+digest+'; original bytes: '+str(len(content))+'\n\n').encode())
            output.extend(content)
            output.extend(b'\n\n===== End source notice =====\n\n')
        manifest['components'].append(component)
    return bytes(output),(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n').encode()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check',action='store_true',help='Fail if tracked notices differ from cached pinned originals')
    args=parser.parse_args()
    contents=generate()
    paths=[ROOT/'THIRD_PARTY_NOTICES.txt',ROOT/'THIRD_PARTY_NOTICES.json']
    for path,data in zip(paths,contents):
        if args.check:
            if not path.is_file() or path.read_bytes()!=data:
                raise SystemExit('Third-party notices missing or stale; regenerate with /usr/bin/python3 service/tools/notices.py')
        else:
            path.write_bytes(data)
    print('Verified third-party notices' if args.check else 'Generated third-party notices')


if __name__=='__main__': main()
