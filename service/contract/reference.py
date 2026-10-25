"""Stateful endpoint reference capture with explicit, narrow canonicalization."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sys
from harness import Service, exact, ROOT

HERE=Path(__file__).resolve().parent
CLOCK='2030-02-03T04:05:06Z'
ID_FIELDS={'id','item_id','person_id','entity_id','profile_id','legacy_tag_id',
           'feed_id','series_id','intake_id','atlas_id'}
PATH_FIELDS={'source_path','snapshot_root','path'}


class Canonical:
    def __init__(self, service):
        self.service=service
        self.identities={}
        self.reverse={}
        self.paths={}
        self.names={}

    def bind(self, actual, symbolic):
        if not isinstance(actual,str) or not actual:
            raise AssertionError('Expected a generated identity string')
        if actual in self.identities and self.identities[actual]!=symbolic:
            raise AssertionError('Identity changed meaning')
        if symbolic in self.reverse and self.reverse[symbolic]!=actual:
            raise AssertionError('Distinct identities collapsed')
        self.identities[actual]=symbolic
        self.reverse[symbolic]=actual

    def value(self, data, key=None):
        if isinstance(data,dict):
            return {k:self.value(v,k) for k,v in data.items()}
        if isinstance(data,list):
            return [self.value(v,key) for v in data]
        if isinstance(data,str):
            if key in ID_FIELDS and data in self.identities:
                return self.identities[data]
            if key=='event_key' and data.startswith('import:'):
                suffix=data[len('import:'):]
                if suffix not in self.identities:
                    raise AssertionError('Unknown import event identity')
                return 'import:'+self.identities[suffix]
            if key in PATH_FIELDS:
                if data not in self.paths:
                    raise AssertionError('Unreviewed path in reference capture')
                return self.paths[data]
            if key=='name' and data in self.names:
                return self.names[data]
        return data


def capture(command=None):
    service=Service(command=command,clock=CLOCK,scenario='stateful-reference')
    result={}
    try:
        c=Canonical(service)
        health=service.get('/health')
        exact(service.pid,health['pid'])
        exact(str(service.library),health['library'])
        result['health']={k:v for k,v in health.items() if k not in ('pid','library')}
        original='Reference café\r\n東京'
        imported=service.tool('import_item',title='Reference document',summary='Reference summary',
            kind='document',date='2026-01-01',original_text=original,original_name='reference.txt',
            source_key='google-doc:reference',doc_urls=['https://docs.google.com/document/d/reference/edit'])
        ident=imported['id']
        c.bind(ident,'item:reference')
        for tag in service.tool('list_tags'):
            c.bind(tag['id'],'tag:'+tag['category']+':'+tag['name'])
        result['import']=c.value(imported)
        item=service.tool('get_item',item_id=ident)
        exact(service.library/'items'/ident/'originals'/'reference.txt',Path(item['source_path']))
        c.paths[item['source_path']]='<LIB>/items/<reference>/originals/reference.txt'
        result['before']=c.value(item)
        status,_,body=service.request('/api/source/'+ident)
        exact(200,status);exact(original.encode(),body)
        result['source_before']=body.hex()
        service.tool('set_starred',item_id=ident,starred=True)
        service.tool('set_refresh_requested',item_id=ident,requested=True)
        revision=service.tool('get_item',item_id=ident)['revision']
        text='Changed reference source\n終'
        refresh=service.tool('refresh_document',item_id=ident,expected_revision=revision,
            source={'file_id':'reference','title':'Reference document','created_time':'2026-01-01T00:00:00Z',
                    'modified_time':'2026-02-01T00:00:00Z','version':'2'},
            original_text=text,summary='Updated reference summary')
        after=service.get('/api/item/'+ident)
        exact(service.library/'items'/ident/'originals',Path(after['source_path']).parent)
        name=Path(after['source_path']).name
        if not re.fullmatch(r'google-refresh-[0-9a-f-]{36}\.txt',name):
            raise AssertionError('Unexpected generated snapshot filename')
        c.paths[after['source_path']]='<LIB>/items/<reference>/originals/<refresh>.txt'
        c.names[name]='google-refresh-<snapshot>.txt'
        entries=service.get('/api/history/'+ident)['entries']
        for entry in entries:
            exact(True,bool(re.fullmatch(r'[0-9a-f]{64}',entry['id'])))
            if entry['kind']=='summary':
                label='summary:'+str(entry['revision'])
            else:
                label='original:'+('updated' if entry['current'] else 'initial')
            c.bind(entry['id'],'history:'+label)
        result['refresh']=c.value(refresh)
        result['after']=c.value(after)
        result['history']=c.value(entries)
        result['history_bytes']={}
        for entry in entries:
            status,_,body=service.request('/api/history/'+ident+'/'+entry['id'])
            exact(200,status)
            result['history_bytes'][c.identities[entry['id']]]=body.hex()
        result['title_search']=c.value(service.get('/api/items?q=Reference&search_scope=title'))
        result['content_search']=c.value(service.get('/api/items?q=Changed&search_scope=full_text'))
        changes=service.get('/api/changed-documents')
        exact(1,len(changes))
        result['changes']=c.value(changes)
        service.post('/api/changes-seen',{'item_id':ident,'through_id':changes[0]['latest_change_id']})
        result['seen']=c.value(service.get('/api/changed-documents'))
        result['activity']=c.value(service.tool('list_activity'))
        service.restart(clock=CLOCK)
        result['after_restart']=c.value(service.tool('get_item',item_id=ident))
        service.tool('delete_item',item_id=ident,expected_revision=result['after_restart']['revision'])
        result['trash']=c.value(service.tool('list_trash'))
        service.tool('restore_item',item_id=ident)
        result['restored']=c.value(service.tool('get_item',item_id=ident))
        return result
    finally:
        service.close()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--command',help='JSON argv; defaults to the disposable Go backend')
    parser.add_argument('--write-baseline',action='store_true',help='Explicit reviewed baseline creation; never used in validation')
    args=parser.parse_args()
    command=json.loads(args.command) if args.command else None
    if args.write_baseline:
        if args.command or os.environ.get('ATLAS_CONTRACT_COMMAND'):
            parser.error('Baseline creation forbids command or inherited environment overrides.')
        command=None
    first=capture(command)
    second=capture(command)
    exact(first,second)
    baseline=HERE/'fixtures'/'reference.json'
    if args.write_baseline:
        baseline.parent.mkdir(exist_ok=True)
        baseline.write_text(json.dumps(first,ensure_ascii=False,indent=2)+'\n')
    else:
        exact(json.loads(baseline.read_text()),first)
    print('PASS: repeated endpoint reference capture; exact normalized JSON, source/history bytes, revisions, events, ordering and restart.')


if __name__=='__main__':
    main()
