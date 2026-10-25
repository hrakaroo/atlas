"""Authenticated HTTP-only client. Never imports a backend or opens SQLite."""
import json
import os
import secrets
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import Request, ProxyHandler, build_opener

DEFAULT_LIBRARY = Path(__file__).resolve().parents[1] / 'library'
DEFAULT_PORT = 47831
PROTOCOL_VERSION = '2025-06-18'


def connection_path(library):
    return Path(library).resolve() / '.service' / 'connection.json'


def load_connection(library=DEFAULT_LIBRARY):
    try:
        connection = json.loads(connection_path(library).read_text())
    except (OSError, ValueError) as error:
        raise ValueError('Atlas service is not configured. Run scripts/service.py install.') from error
    url = urlsplit(connection.get('url', ''))
    if (url.scheme != 'http' or url.hostname != '127.0.0.1' or not url.port
            or url.path or url.query or url.fragment or url.username or url.password
            or not connection.get('token') or connection.get('library') != str(Path(library).resolve())):
        raise ValueError('Invalid Atlas service connection settings.')
    return connection


def save_connection(library, port=DEFAULT_PORT, token=None):
    path = connection_path(library)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.parent.chmod(0o700)
    if token is None and path.exists():
        token = load_connection(library)['token']
    connection = {'url': 'http://127.0.0.1:' + str(port),
                  'token': token or secrets.token_urlsafe(32), 'library': str(Path(library).resolve())}
    temp = path.with_suffix('.tmp')
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(connection, stream)
    temp.chmod(0o600)
    temp.replace(path)
    return connection


def request(library, path, payload=None, timeout=120):
    connection = load_connection(library)
    headers = {'Authorization': 'Bearer ' + connection['token'],
               'Accept': 'application/json, text/event-stream', 'MCP-Protocol-Version': PROTOCOL_VERSION}
    data = None
    if payload is not None:
        data = json.dumps(payload, ensure_ascii=False).encode('utf-8')
        headers['Content-Type'] = 'application/json'
    req = Request(connection['url'] + path, data=data, headers=headers)
    try:
        # Loopback traffic must not be routed through an environment HTTP proxy.
        with build_opener(ProxyHandler({})).open(req, timeout=timeout) as response:
            body = response.read()
            return json.loads(body) if body else None
    except HTTPError as error:
        raise ValueError('Atlas service returned HTTP ' + str(error.code) + '.') from error
    except (URLError, TimeoutError, OSError) as error:
        raise ValueError('Atlas service is unavailable. Run scripts/service.py status or restart.') from error


def call_tool(name, arguments, library=DEFAULT_LIBRARY):
    response = request(library, '/mcp', {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call',
                                       'params': {'name': name, 'arguments': arguments}})
    if 'error' in response:
        raise ValueError(response['error']['message'])
    result = response['result']
    if result.get('isError'):
        raise ValueError(result['content'][0]['text'])
    return json.loads(result['content'][0]['text'])
