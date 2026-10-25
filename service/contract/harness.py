"""Endpoint-only Atlas contract infrastructure; no application or SQLite imports."""
import http.client
import base64
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
OBSERVED = []
CAPTURES = []


class ToolError(AssertionError):
    """Application-level MCP error (not a transport exception)."""


def exact(expected, actual, path="$"):
    """Fail on value, type, shape or order differences; never coerce JSON values."""
    if type(expected) is not type(actual):
        raise AssertionError(f"{path}: type {type(expected).__name__} != {type(actual).__name__}")
    if isinstance(expected, dict):
        if expected.keys() != actual.keys():
            raise AssertionError(f"{path}: object keys differ")
        for key in expected:
            exact(expected[key], actual[key], path + "." + str(key))
    elif isinstance(expected, list):
        if len(expected) != len(actual):
            raise AssertionError(f"{path}: list lengths differ")
        for i, (left, right) in enumerate(zip(expected, actual)):
            exact(left, right, f"{path}[{i}]")
    elif expected != actual:
        raise AssertionError(f"{path}: values differ")


class IdMap:
    """Explicit bijection for generated identities; never sort/rewrite opaque payloads."""
    def __init__(self):
        self.forward = {}
        self.reverse = {}

    def bind(self, reference, candidate):
        if not isinstance(reference, str) or not reference or not isinstance(candidate, str) or not candidate:
            raise AssertionError("Nonempty string identities required")
        if reference in self.forward and self.forward[reference] != candidate:
            raise AssertionError("Identity mapping changed")
        if candidate in self.reverse and self.reverse[candidate] != reference:
            raise AssertionError("Distinct identities collapsed")
        self.forward[reference] = candidate
        self.reverse[candidate] = reference

    def resolve(self, reference):
        return self.forward[reference]


class Service:
    """Own one disposable process and root, never connect to a live installation."""
    def __init__(self, command=None, library=None, clock=None, scenario=None,
                 library_name="library", env_overrides=None):
        self.scenario = scenario or "standalone"
        raw = os.environ.get("ATLAS_CONTRACT_COMMAND")
        self.command = command if command is not None else json.loads(raw) if raw else \
                       [str(ROOT / "service" / ".work" / "atlas-service"), "--contract-mode", "--owner-name", "John Smith", "--timezone", "America/Los_Angeles"]
        if not isinstance(self.command, (list, tuple)) or not self.command or not all(isinstance(x, str) for x in self.command):
            raise ValueError("Contract command must be a nonempty JSON argv")
        self.command = list(self.command)
        self._temporary = tempfile.TemporaryDirectory(prefix="atlas-contract-")
        # External roots are permitted only under this process-owned scratch directory.
        if library is not None:
            self._temporary.cleanup()
            raise ValueError("Externally supplied library roots are prohibited")
        if (not isinstance(library_name, str) or not library_name or
                library_name in (".", "..") or Path(library_name).name != library_name or
                "/" in library_name or "\\" in library_name):
            self._temporary.cleanup()
            raise ValueError("Library name must be one safe path component")
        self.env_overrides = dict(env_overrides or {})
        self.library = Path(self._temporary.name).resolve() / library_name
        self.library.mkdir()
        (self.library.parent / ".atlas-contract-fixture").write_text("atlas-contract-v1")
        self.process = None
        self.connection = None
        self.log = None
        self.clock = clock
        self._rpc_id = 0
        try:
            self.start()
        except BaseException:
            self.close()
            raise

    @property
    def pid(self):
        return self.process.pid

    def start(self):
        if self.process is not None and self.process.poll() is None:
            raise RuntimeError("Test process already running")
        env = os.environ.copy()
        for key, value in self.env_overrides.items():
            if not isinstance(key, str) or not key or (value is not None and not isinstance(value, str)):
                raise ValueError("Environment overrides require string keys and string or None values")
            if value is None:
                env.pop(key, None)
            else:
                env[key] = value
        env.pop("ATLAS_CONTRACT_CLOCK", None)
        if self.clock is not None:
            env["ATLAS_CONTRACT_CLOCK"] = str(self.clock)
        self.log = open(Path(self._temporary.name) / "service.log", "ab")
        argv = self.command + ["--library", str(self.library), "--port", "0"]
        self.process = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=self.log,
                                        stderr=subprocess.STDOUT, env=env, cwd=ROOT)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                self.stop()
                raise RuntimeError("Disposable service exited before readiness")
            try:
                candidate = json.loads((self.library / ".service" / "connection.json").read_text())
                location = urlsplit(candidate["url"])
                if (candidate.get("library") != str(self.library.resolve()) or
                    location.scheme != "http" or location.hostname != "127.0.0.1" or
                    not location.port or location.path or location.query or location.fragment or
                    location.username or location.password or not candidate.get("token")):
                    raise ValueError("Invalid disposable connection settings")
                self.connection = candidate
                status, _, body = self.request("/health")
                if status == 200 and json.loads(body)["pid"] == self.pid:
                    return
            except (OSError, ValueError, KeyError, http.client.HTTPException):
                pass
            time.sleep(0.025)
        self.stop()
        raise RuntimeError("Disposable service readiness timed out")

    def stop(self):
        if self.process is not None and self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        if self.log:
            self.log.close()
            self.log = None

    def close(self):
        self.stop()
        self._temporary.cleanup()

    def restart(self, clock=None, command=None, env_overrides=None):
        if command is not None and (not isinstance(command, (list, tuple)) or not command or
                                    not all(isinstance(x, str) for x in command)):
            raise ValueError("Contract command must be a nonempty JSON argv")
        self.stop()
        if clock is not None:
            self.clock = clock
        if command is not None:
            self.command = list(command)
        if env_overrides is not None:
            self.env_overrides = dict(env_overrides)
        self.start()

    def restore_backup(self, path):
        """Fixture orchestration only: reopen an endpoint-created own-root backup."""
        source = Path(path).resolve()
        allowed = (self.library / "backups").resolve()
        if not source.is_relative_to(allowed) or source == allowed or not source.is_dir():
            raise ValueError("Only this disposable library's endpoint-created backup is allowed")
        if any(p.is_symlink() for p in source.rglob("*")) or Path(path).is_symlink():
            raise ValueError("Symlinks are prohibited in disposable backup fixtures")
        self.stop()
        destination = self.library.parent / "restored-library"
        shutil.copytree(source, destination)
        shutil.rmtree(self.library)
        destination.rename(self.library)
        self.start()

    def reopen_backup(self, path):
        return self.restore_backup(path)

    def request(self, path, payload=None, method=None, headers=None):
        if not path.startswith("/") or path.startswith("//"):
            raise ValueError("A local endpoint path is required")
        values = {
            "Authorization": "Bearer " + self.connection["token"],
            "Accept": "application/json, text/event-stream",
            "MCP-Protocol-Version": "2025-06-18",
        }
        data = payload
        if payload is not None and not isinstance(payload, bytes):
            data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            values["Content-Type"] = "application/json"
        elif isinstance(payload, bytes):
            values["Content-Type"] = "application/json"
        for key, value in (headers or {}).items():
            if value is None:
                values.pop(key, None)
            else:
                values[key] = value
        verb = method or ("POST" if payload is not None else "GET")
        host = urlsplit(self.connection["url"])
        conn = http.client.HTTPConnection("127.0.0.1", host.port, timeout=15)
        try:
            conn.request(verb, path, body=data, headers=values)
            response = conn.getresponse()
            body = response.read()
            metadata = dict(response.getheaders())
            # Record route/tool identity only; never credentials or payload contents.
            observation = {"method": verb, "path": urlsplit(path).path, "status": response.status}
            if path == "/mcp" and isinstance(payload, dict):
                observation["rpc_method"] = payload.get("method")
                if payload.get("method") == "tools/call":
                    observation["tool"] = payload.get("params", {}).get("name")
            OBSERVED.append(observation)
            if os.environ.get("ATLAS_CONTRACT_CAPTURE") == "1":
                # Synthetic-only diagnostic capture. Not a normalized golden baseline.
                # Token-bearing navigation/HTML are explicitly redacted; never log headers.
                token = self.connection["token"]
                safe_body = body.replace(token.encode(), b"<TEST_TOKEN>")
                mime = metadata.get("Content-Type", "")
                if mime.startswith("application/json"):
                    try:
                        captured = {"json": json.loads(safe_body)}
                    except (ValueError, UnicodeError):
                        captured = {"bytes_base64": base64.b64encode(safe_body).decode("ascii")}
                else:
                    captured = {"bytes_base64": base64.b64encode(safe_body).decode("ascii")}
                if data is None:
                    captured_request = None
                elif isinstance(payload, bytes):
                    captured_request = {"bytes_base64": base64.b64encode(data.replace(token.encode(), b"<TEST_TOKEN>")).decode("ascii")}
                else:
                    captured_request = {"json": json.loads(data.replace(token.encode(), b"<TEST_TOKEN>"))}
                CAPTURES.append({
                    **observation, "scenario": self.scenario, "fixture": str(self.library),
                    "request_path": path.replace(token, "<TEST_TOKEN>"),
                    "request": captured_request,
                    "content_type": mime, "response": captured,
                    "normalization": "None except exact known test token redaction; dynamic values remain explicit",
                })
            return response.status, metadata, body
        finally:
            conn.close()

    def get(self, path):
        code, _, body = self.request(path)
        if code != 200:
            raise AssertionError(f"GET {urlsplit(path).path} returned {code}")
        return json.loads(body)

    def post(self, path, payload):
        code, _, body = self.request(path, payload)
        if code != 200:
            raise AssertionError(f"POST {path} returned {code}: {body.decode('utf-8', errors='replace')}")
        return json.loads(body)

    def rpc(self, method, params=None):
        self._rpc_id += 1
        request = {"jsonrpc": "2.0", "id": self._rpc_id, "method": method}
        if params is not None:
            request["params"] = params
        result = self.post("/mcp", request)
        exact(self._rpc_id, result.get("id"))
        return result

    def tool(self, tool_name, **arguments):
        response = self.rpc("tools/call", {"name": tool_name, "arguments": arguments})
        if "error" in response:
            raise ToolError(response["error"]["message"])
        result = response["result"]
        if result.get("isError"):
            raise ToolError(result["content"][0]["text"])
        return json.loads(result["content"][0]["text"])


class EndpointCase(unittest.TestCase):
    def setUp(self):
        self.service = Service(scenario=self.id())
        self.addCleanup(self.service.close)

    def request(self, *args, **kwargs):
        return self.service.request(*args, **kwargs)

    def get(self, path):
        return self.service.get(path)

    def post(self, path, payload):
        return self.service.post(path, payload)

    def tool(self, tool_name, **arguments):
        return self.service.tool(tool_name, **arguments)

    def restart(self, clock=None):
        self.service.restart(clock=clock)
