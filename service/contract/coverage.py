"""Audit executed transport coverage; never infer domain assertions from a request."""
import argparse
import json
from pathlib import Path


def audit(report, inventory):
    captures=report.get('diagnostic_captures',[])
    observations=report['observed_requests']
    successful_tools=set()
    for capture in captures:
        response=capture.get('response',{}).get('json',{})
        if capture.get('rpc_method')=='tools/call' and isinstance(response,dict):
            result=response.get('result',{})
            if capture['status']==200 and isinstance(result,dict) and not result.get('isError') and 'content' in result:
                successful_tools.add(capture.get('tool'))
    missing_tools=sorted({x['name'] for x in inventory['mcp_tools']}-successful_tools)
    missing_routes=[]
    for route in inventory['http_route_branches']:
        method,path=route['method'],route['path_or_prefix']
        expected=405 if method=='GET' and path=='/mcp' else 200
        found=any(o['method']==method and o['status']==expected and
            (o['path'].startswith(path) if path.endswith('/') and path!='/' else o['path']==path)
            for o in observations)
        if not found:
            missing_routes.append(method+' '+path)
    # Current/history read routes were additive after the original source inventory.
    for count,label in ((3,'history list'),(4,'history read')):
        if not any(o['method']=='GET' and o['status']==200 and o['path'].startswith('/api/history/')
                   and len(o['path'].strip('/').split('/'))==count for o in observations):
            missing_routes.append(label)
    return {'successful_advertised_tools':len(successful_tools & {x['name'] for x in inventory['mcp_tools']}),
            'missing_tools':missing_tools,'missing_routes':missing_routes,
            'tests_passed':report['passed'],
            'scope':'Executed successful transport coverage only. Domain acceptance still requires scenario assertions and independent review.'}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('report',type=Path)
    args=parser.parse_args()
    root=Path(__file__).resolve().parents[2]
    result=audit(json.loads(args.report.read_text()),
                 json.loads((root/'service/contract/inventory.json').read_text()))
    print(json.dumps(result,indent=2))
    return 0 if result['tests_passed'] and not result['missing_tools'] and not result['missing_routes'] else 1


if __name__=='__main__':
    raise SystemExit(main())
