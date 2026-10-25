"""Read Google Doc links from Chrome's latest saved Default-profile session.

No browser interaction, decryption, imports or state changes. Output is a saved
snapshot, not a claim that Chrome is running or that the tabs are live.
"""
import argparse
import bisect
import json
import re
import struct
import sys
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import parse_qs, urlencode, urlsplit

class SessionError(ValueError):
    pass

def integer(data, offset=0):
    if offset + 4 > len(data):
        raise SessionError("Incomplete session field")
    return struct.unpack_from("<i", data, offset)[0]

def navigation(data):
    tab_id, index = integer(data, 4), integer(data, 8)
    n = integer(data, 12)
    if n < 0 or 16 + n > len(data):
        raise SessionError("Incomplete navigation URL")
    url = data[16:16+n].decode("utf-8")
    offset = (16+n+3)//4*4
    n = integer(data, offset)
    if n < 0 or offset + 4 + n*2 > len(data):
        raise SessionError("Incomplete navigation title")
    title = data[offset+4:offset+4+n*2].decode("utf-16-le")
    return tab_id, index, {"title": title, "url": url}

def parse_session(data):
    if data[:8] != b"SNSS\x03\x00\x00\x00":
        raise SessionError("Unsupported session signature/version; no imports allowed")
    tabs, windows, discarded = {}, set(), set()
    def tab(ident):
        return tabs.setdefault(ident, {"navigations": {}})
    pos = 8
    while pos < len(data):
        if pos+2 > len(data):
            raise SessionError("Truncated session command")
        size = struct.unpack_from("<H", data, pos)[0]
        pos += 2
        if not size or pos+size > len(data):
            raise SessionError("Incomplete session command")
        command, payload = data[pos], data[pos+1:pos+size]
        pos += size
        if command == 0:
            window, ident = integer(payload), integer(payload, 4)
            windows.add(window)
            tab(ident)["window"] = window
        elif command == 6:
            ident, index, entry = navigation(payload)
            tab(ident)["navigations"][index] = entry
        elif command == 7:
            tab(integer(payload))["selected"] = integer(payload, 4)
        elif command == 2:
            tab(integer(payload))["order"] = integer(payload, 4)
        elif command == 16:
            tabs.pop(integer(payload), None)
        elif command == 17:
            ident = integer(payload)
            windows.discard(ident)
            discarded.add(ident)
        elif command in (5, 11, 24):
            ident, index = integer(payload), integer(payload, 4)
            entry = tab(ident)
            navs = entry["navigations"]
            if command == 5:
                entry["navigations"] = {i: v for i, v in navs.items() if i < index}
            else:
                start, count = (0, index) if command == 11 else (index, integer(payload, 8))
                if start < 0 or count <= 0:
                    raise SessionError("Invalid navigation pruning")
                entry["navigations"] = {i if i < start else i-count: v
                    for i, v in navs.items() if not start <= i < start+count}
                selected = entry.get("selected", 0)
                if start <= selected < start+count:
                    entry["selected"] = start-1
                elif selected >= start+count:
                    entry["selected"] = selected-count
        elif command not in set(range(38)) | {255}:
            raise SessionError("Unknown session command: " + str(command))
    rows = []
    for ident, entry in tabs.items():
        if entry.get("window") not in windows or entry.get("window") in discarded:
            continue
        indexes = sorted(entry["navigations"])
        if not indexes:
            raise SessionError("Saved tab has no navigation")
        # Chromium restores the closest index at/after the selected index,
        # falling back to the last navigation when it lies beyond the end.
        selected = entry.get("selected", -1)
        index = indexes[min(bisect.bisect_left(indexes, selected), len(indexes)-1)]
        rows.append(dict(entry["navigations"][index], window=entry["window"],
                         order=entry.get("order", 0), tab_id=ident))
    return sorted(rows, key=lambda row: (row["window"], row["order"]))

def document_title(title):
    return re.sub(r"\s+-\s+Google Docs\s*$", "", title or "", flags=re.IGNORECASE).strip()

def is_titled_document(title):
    normalized = " ".join(document_title(title).split()).casefold()
    return bool(normalized) and "untitled document" not in normalized

def google_documents(rows):
    docs = {}
    matching = 0
    for row in rows:
        url = urlsplit(row["url"])
        match = re.fullmatch(r"/document/(?:u/\d+/)?d/([A-Za-z0-9_-]+)(?:/.*)?", url.path)
        if url.scheme != "https" or url.hostname != "docs.google.com" or not match:
            continue
        ident = match[1]
        if ident == "e":  # Published documents are not native file IDs.
            continue
        matching += 1
        if not is_titled_document(row["title"]):
            continue
        canonical = "https://docs.google.com/document/d/" + ident + "/edit"
        resourcekey = parse_qs(url.query).get("resourcekey", [])
        if resourcekey:
            canonical += "?" + urlencode({"resourcekey": resourcekey[0]})
        if ident not in docs or (resourcekey and "resourcekey=" not in docs[ident]["url"]):
            docs[ident] = {"file_id": ident, "title": document_title(row["title"]),
                           "url": canonical}
    return matching, sorted(docs.values(), key=lambda row: row["title"].casefold())

def read_snapshot(root, profile):
    if Path(profile).name != profile or profile in (".", ".."):
        raise SessionError("Profile must be a single directory name")
    folder = root / profile / "Sessions"
    candidates = [p for p in folder.glob("Session_*") if p.name[8:].isdigit()]
    if not candidates:
        raise SessionError("No readable saved session in configured profile")
    latest = max(candidates, key=lambda p: int(p.name[8:]))
    for attempt in range(3):
        before = latest.stat()
        data = latest.read_bytes()
        after = latest.stat()
        if (before.st_size, before.st_mtime_ns) == (after.st_size, after.st_mtime_ns):
            break
    else:
        raise SessionError("Session still changing; retry next scheduled run")
    rows = parse_session(data)
    count, documents = google_documents(rows)
    return {"snapshot": True, "profile": profile, "session_file": str(latest),
            "modified_time": datetime.fromtimestamp(after.st_mtime, timezone.utc).isoformat(),
            "tabs": len(rows), "windows": len({r["window"] for r in rows}),
            "google_doc_tabs": count, "unique_documents": len(documents),
            "documents": documents}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--chrome-root", type=Path,
        default=Path.home() / "Library/Application Support/Google/Chrome")
    parser.add_argument("--profile", default="Default")
    args = parser.parse_args()
    try:
        result = read_snapshot(args.chrome_root, args.profile)
    except (OSError, ValueError, struct.error) as error:
        print(json.dumps({"error": str(error), "documents": []}), file=sys.stderr)
        return 1
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0

if __name__ == "__main__":
    sys.exit(main())
