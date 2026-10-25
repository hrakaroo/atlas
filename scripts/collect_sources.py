"""Collect sources and hand the entire eligible batch to Atlas's intake API.

Collectors do not query library inventories, choose new sources, summarize,
restore removed items, or write library files. Library owns those decisions.
"""
import argparse
import codecs
import datetime as dt
import json
import re
import sys
import time
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'app'))
from service_client import call_tool
from chrome_documents import read_snapshot

def read_transcript(path):
    before = path.stat()
    if before.st_size==0 or time.time()-before.st_mtime<600:
        return None
    data = path.read_bytes()
    after = path.stat()
    if (before.st_size,before.st_mtime_ns)!=(after.st_size,after.st_mtime_ns):
        return None
    if data.startswith(codecs.BOM_UTF8):
        text = data.decode('utf-8-sig')
    elif data.startswith((codecs.BOM_UTF16_LE,codecs.BOM_UTF16_BE)):
        text = data.decode('utf-16')
    else:
        text = data.decode('utf-8')
    return text if text.strip() else None

def zoom_sources(root):
    if not root.is_dir():
        raise ValueError('Zoom source folder is unavailable: '+str(root))
    sources, issues = [], []
    paths = sorted(root.rglob('*'),key=lambda p:p.stat().st_mtime if p.is_file() else 0,reverse=True)
    for path in paths:
        if not path.is_file() or path.is_symlink():
            continue
        name = path.name.casefold()
        allowed = (name.endswith('.txt') and ('transcript' in name or name=='meeting_saved_closed_caption.txt')) or path.suffix.casefold() in ('.vtt','.srt')
        if not allowed:
            continue
        try:
            text = read_transcript(path)
            if not text:
                continue
            if path.suffix.casefold() in ('.vtt','.srt') and not (text.lstrip().startswith('WEBVTT') or re.search(r'\d{1,2}:\d{2}:\d{2}[.,]\d+\s*-->',text)):
                continue
            relative = path.relative_to(root).as_posix()
            folder = next((part for part in path.relative_to(root).parts[:-1] if re.match(r'^\d{4}-\d{2}-\d{2}',part)),path.parent.name)
            match = re.match(r'^(\d{4}-\d{2}-\d{2})\s+(\d{2})[.:-](\d{2})[.:-](\d{2})\s*(.*)',folder)
            title = match[5] if match else folder
            source = {'type':'zoom','source_key':'zoom-transcript:'+relative,'title':title,
                      'original_name':path.name,'original_text':text,'relative_path':relative}
            if match:
                source['date']=match[1]
                source['session_time']=match[1]+'T'+':'.join(match.group(2,3,4))+' America/Los_Angeles'
            sources.append(source)
        except (OSError,UnicodeError) as error:
            issues.append({'path':str(path.relative_to(root)),'reason':str(error)})
    return sources,issues

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source_type',choices=['chrome','zoom'])
    parser.add_argument('--library',type=Path,default=Path(__file__).resolve().parents[1]/'library')
    parser.add_argument('--zoom-root',type=Path,default=Path.home()/'Documents/Zoom')
    parser.add_argument('--chrome-root',type=Path,default=Path.home()/'Library/Application Support/Google/Chrome')
    parser.add_argument('--profile',default='Default')
    args = parser.parse_args()
    try:
        if args.source_type=='chrome':
            snapshot = read_snapshot(args.chrome_root,args.profile)
            sources = [dict(doc,type='google_doc') for doc in snapshot['documents']]
            issues = []
        else:
            sources,issues = zoom_sources(args.zoom_root)
        result = call_tool('submit_sources', {'sources': sources}, args.library)
        result['issues']=issues
        result['submitted']=len(sources)
        result['work']=call_tool('list_intake', {'source_type': 'google_doc' if args.source_type=='chrome' else 'zoom', 'limit': 5}, args.library)
        # Full transcript bodies stay in Atlas's queue; get_intake reads a
        # selected new source only when content processing is necessary.
        print(json.dumps(result,ensure_ascii=False))
    except (OSError,ValueError) as error:
        print(json.dumps({'error':str(error)}),file=sys.stderr)
        return 1
    return 0

if __name__=='__main__':
    sys.exit(main())
