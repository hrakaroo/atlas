"""Call the shared Atlas service. Pass tool name as argv[1], JSON on stdin."""
import json
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'app'))
from service_client import call_tool

if __name__ == '__main__':
    try:
        print(json.dumps(call_tool(sys.argv[1], json.load(sys.stdin)), ensure_ascii=False))
    except (ValueError, OSError) as error:
        print(json.dumps({'error': str(error)}), file=sys.stderr)
        sys.exit(1)
