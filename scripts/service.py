"""Manage the one per-user Atlas service and its local MCP registration."""
import argparse
import json
import os
import plistlib
import re
import subprocess
import sys
import time
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'app'))
from service_client import DEFAULT_LIBRARY, DEFAULT_PORT, connection_path, load_connection, save_connection, request

PROJECT = Path(__file__).resolve().parents[1]
LABEL = 'local.atlas.server'
DOMAIN = 'gui/' + str(os.getuid())
PLIST = Path.home() / 'Library' / 'LaunchAgents' / (LABEL + '.plist')
TARGET = DOMAIN + '/' + LABEL


def launchctl(*args, check=True):
    return subprocess.run(['/bin/launchctl', *args], check=check, capture_output=True, text=True)


def wait_ready(library, previous_pid=None):
    for attempt in range(60):
        try:
            health = request(library, '/health', timeout=1)
            if health['pid'] != previous_pid: return health
            time.sleep(0.2)
        except ValueError:
            time.sleep(0.2)
    raise ValueError('Atlas service did not become ready. See library/.service/stderr.log.')


def configure_mcp(library):
    settings = load_connection(library)
    request(library, '/health', timeout=2)  # Never switch clients to an unavailable service.
    config = Path(os.environ.get('CODEX_HOME', str(Path.home() / '.codex'))) / 'config.toml'
    original = config.read_text() if config.exists() else ''
    pattern = r'(?m)^\[mcp_servers\.atlas\][^\n]*\n[\s\S]*?(?=^\[|\Z)'
    match = re.search(pattern, original)
    retained = []
    if match:
        retained = [line for line in match.group().splitlines()[1:] if line.strip() and not re.match(
            r'^\s*(enabled|command|args|cwd|env|url|http_headers|bearer_token_env_var|http_headers_helper)\s*=', line)]
    section = ('[mcp_servers.atlas]\nenabled = true\nurl = ' + json.dumps(settings['url'] + '/mcp')
               + '\nhttp_headers = { Authorization = ' + json.dumps('Bearer ' + settings['token']) + ' }\n')
    if retained: section += '\n'.join(retained) + '\n'
    section += '\n'
    updated = original[:match.start()] + section + original[match.end():] if match else original.rstrip() + '\n\n' + section
    config.parent.mkdir(parents=True, exist_ok=True)
    if updated != original:
        backup = config.with_name('config.toml.before-atlas-http')
        if not backup.exists():
            fd = os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'w') as stream: stream.write(original)
        temporary = config.with_name('config.toml.atlas-tmp')
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, 'w') as stream: stream.write(updated)
        temporary.replace(config)
    marker = connection_path(library).parent / 'enabled'
    marker.touch(mode=0o600)
    return 'Updated the existing atlas MCP registration; restart ChatGPT to reconnect.'


def launch_spec(library, port, backend='go', app=None, owner_name=None, timezone=None):
    """Pure concrete Go launch specification; never writes or registers anything."""
    if backend != 'go':
        raise ValueError('Atlas supports only the Go backend.')
    if not isinstance(port, int) or not 1 <= port <= 65535:
        raise ValueError('Choose a port between 1 and 65535.')
    if not isinstance(owner_name, str) or not owner_name.strip() or not timezone:
        raise ValueError('Go requires --owner-name and --timezone.')
    from zoneinfo import ZoneInfo, ZoneInfoNotFoundError
    try:
        ZoneInfo(timezone)
    except (ZoneInfoNotFoundError, ValueError, TypeError):
        raise ValueError('Choose a valid IANA timezone.')
    root = Path(library).resolve()
    bundle = Path(app) if app is not None else PROJECT/'build/Atlas.app'
    if not bundle.is_absolute():
        raise ValueError('--app requires an absolute Atlas.app path.')
    executable = bundle/'Contents/Library/LoginItems/AtlasService.app/Contents/MacOS/AtlasService'
    native = bundle/'Contents/Resources/bin/atlas-service'
    arguments = [str(executable), '--service', '--go-service', str(native),
                 '--owner-name', owner_name, '--timezone', timezone,
                 '--library', str(root), '--port', str(port)]
    runtime = connection_path(root).parent
    return {'Label': LABEL, 'AssociatedBundleIdentifiers': ['local.atlas.go-preview.service'],
            'ProgramArguments': arguments, 'WorkingDirectory': str(root),
            'RunAtLoad': True, 'KeepAlive': True, 'ThrottleInterval': 10,
            'ProcessType': 'Background', 'ExitTimeOut': 30,
            'StandardOutPath': str(runtime/'stdout.log'), 'StandardErrorPath': str(runtime/'stderr.log')}


def install(library, port, backend='go', app=None, owner_name=None, timezone=None):
    spec = launch_spec(library, port, backend, app, owner_name, timezone)
    if not Path(spec['ProgramArguments'][0]).exists(): raise ValueError('Build Atlas first with zsh build.sh.')
    bundle = Path(app) if app is not None else PROJECT/'build/Atlas.app'
    if not (bundle/'Contents/Resources/bin/atlas-service').is_file():
        raise ValueError('Build the native Go service first.')
    subprocess.run(['/usr/bin/codesign', '--verify', '--deep', '--strict', str(bundle)], check=True, capture_output=True)
    if connection_path(library).exists():
        existing = load_connection(library)
        if existing['url'] != 'http://127.0.0.1:' + str(port):
            raise ValueError('Stop the existing service before changing its port.')
    settings = save_connection(library, port)
    PLIST.parent.mkdir(parents=True, exist_ok=True)
    current = plistlib.loads(PLIST.read_bytes()) if PLIST.exists() else None
    if current and current != spec:
        launchctl('bootout', TARGET, check=False)
    PLIST.write_bytes(plistlib.dumps(spec)); PLIST.chmod(0o600)
    if launchctl('print', TARGET, check=False).returncode:
        launchctl('enable', TARGET)
        result = launchctl('bootstrap', DOMAIN, str(PLIST), check=False)
        if result.returncode: raise ValueError('Could not start Atlas service: ' + result.stderr.strip())
    try:
        health = wait_ready(library)
    except ValueError:
        launchctl('bootout', TARGET, check=False)
        raise ValueError('Atlas background service could not start. If macOS requests Documents access for Atlas Service, allow it, then rerun install. See library/.service/stderr.log.')
    print(json.dumps({'service': 'atlas', 'pid': health['pid'], 'url': settings['url'], 'starts_at_login': True}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['install', 'status', 'restart', 'stop', 'start', 'configure-mcp', 'request-access', 'print-launch-spec'])
    parser.add_argument('--library', type=Path, default=DEFAULT_LIBRARY)
    parser.add_argument('--port', type=int, default=DEFAULT_PORT)
    parser.add_argument('--backend', choices=['go'], default='go')
    parser.add_argument('--app', type=Path, help='Absolute app bundle for an isolated launch specification')
    parser.add_argument('--owner-name')
    parser.add_argument('--timezone')
    args = parser.parse_args()
    try:
        if args.action == 'print-launch-spec':
            print(json.dumps(launch_spec(args.library, args.port, args.backend, args.app, args.owner_name, args.timezone), indent=2))
        elif args.action == 'request-access':
            bundle = args.app or PROJECT/'build/Atlas.app'
            helper = bundle/'Contents/Library/LoginItems/AtlasService.app'
            if not helper.exists(): raise ValueError('Build Atlas first with zsh build.sh.')
            subprocess.run(['/usr/bin/open', str(helper), '--args', '--request-access', '--library', str(args.library.resolve())], check=True)
            print('Allow Documents access if macOS asks for Atlas Service, then run install.')
        elif args.action == 'install':
            install(args.library, args.port, args.backend, args.app, args.owner_name, args.timezone)
        elif args.action == 'configure-mcp':
            print(configure_mcp(args.library))
        elif args.action == 'status':
            print(json.dumps(request(args.library, '/health', timeout=2)))
        elif args.action == 'stop':
            result = launchctl('bootout', TARGET, check=False)
            if result.returncode: raise ValueError('Atlas service is not loaded: ' + result.stderr.strip())
            print('Atlas service stopped. Run service.py start to resume.')
        elif args.action == 'restart':
            try: previous_pid = request(args.library, '/health', timeout=1)['pid']
            except ValueError: previous_pid = None
            if launchctl('print', TARGET, check=False).returncode:
                launchctl('bootstrap', DOMAIN, str(PLIST))
            else:
                # Signal the native supervisor, which gracefully stops Go.
                # KeepAlive restarts the job without a bootout/bootstrap race.
                launchctl('kill', 'SIGTERM', TARGET)
            print(json.dumps(wait_ready(args.library, previous_pid)))
        elif args.action == 'start':
            if launchctl('print', TARGET, check=False).returncode:
                launchctl('bootstrap', DOMAIN, str(PLIST))
            else:
                launchctl('kickstart', TARGET)
            print(json.dumps(wait_ready(args.library)))
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
