import importlib.util
import struct
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("chrome_documents", Path(__file__).resolve().parents[1] / "scripts/chrome_documents.py")
chrome = importlib.util.module_from_spec(spec)
spec.loader.exec_module(chrome)

def command(ident, payload):
    return struct.pack("<H", len(payload)+1) + bytes([ident]) + payload

def pair(ident, a, b):
    return command(ident, struct.pack("<ii", a, b))

def nav(tab, index, url, title):
    encoded = url.encode()
    value = struct.pack("<iii", tab, index, len(encoded)) + encoded
    value += b"\0" * (-len(value) % 4)
    value += struct.pack("<i", len(title.encode("utf-16-le"))//2) + title.encode("utf-16-le")
    return command(6, struct.pack("<i", len(value)) + value)

HEADER = b"SNSS\x03\0\0\0"

class ChromeSessionTests(unittest.TestCase):
    def test_selected_page_not_prior_history_and_closed_tabs_excluded(self):
        data = HEADER + pair(0, 1, 11)
        data += nav(11, 0, "https://docs.google.com/document/d/old/edit", "Old")
        data += nav(11, 1, "https://example.com", "Current")
        data += pair(7, 11, 1) + pair(0, 1, 12)
        data += nav(12, 0, "https://docs.google.com/document/d/closed/edit", "Closed")
        data += command(16, struct.pack("<i", 12))
        rows = chrome.parse_session(data)
        self.assertEqual(len(rows), 1)
        self.assertEqual(chrome.google_documents(rows), (0, []))

    def test_duplicates_unicode_and_access_parameter_preserved(self):
        data = HEADER + pair(0, 1, 11) + pair(0, 2, 12)
        data += nav(11, 0, "https://docs.google.com/document/d/abc/edit?tab=t.0", "Résumé 🍎 - Google Docs")
        data += nav(12, 0, "https://docs.google.com/document/u/2/d/abc/edit?resourcekey=key&tab=t.1", "Résumé 🍎 - Google Docs")
        data += pair(7, 11, 0) + pair(7, 12, 0)
        count, docs = chrome.google_documents(chrome.parse_session(data))
        self.assertEqual(count, 2)
        self.assertEqual(len(docs), 1)
        self.assertEqual(docs[0]["title"], "Résumé 🍎")
        self.assertEqual(docs[0]["url"], "https://docs.google.com/document/d/abc/edit?resourcekey=key")

    def test_closed_window_and_published_links_excluded(self):
        data = HEADER + pair(0, 1, 11)
        data += nav(11, 0, "https://docs.google.com/document/d/abc/edit", "Doc")
        data += command(17, struct.pack("<i", 1))
        self.assertEqual(chrome.parse_session(data), [])
        self.assertEqual(chrome.google_documents([{"url":"https://docs.google.com/document/d/e/published/pub","title":"Published"}]), (0, []))

    def test_truncation_unknown_command_and_version_fail_closed(self):
        for data in [HEADER + b"\x10\0\x06", HEADER + command(99, b""), b"SNSS\x04\0\0\0"]:
            with self.assertRaises(chrome.SessionError):
                chrome.parse_session(data)

    def test_pruning_adjusts_selected_navigation(self):
        data = HEADER + pair(0, 1, 11)
        for i in range(3):
            data += nav(11, i, "https://docs.google.com/document/d/doc"+str(i)+"/edit", "Doc "+str(i))
        data += pair(7, 11, 2)
        data += command(24, struct.pack("<iii", 11, 0, 2))
        rows = chrome.parse_session(data)
        self.assertTrue(rows[0]["url"].endswith("doc2/edit"))


class DraftTitleTests(unittest.TestCase):
    def test_drafts_excluded_but_later_titled_tab_is_eligible(self):
        rows = [
            {"url": "https://docs.google.com/document/d/blank/edit", "title": "  "},
            {"url": "https://docs.google.com/document/d/draft/edit", "title": "  UNTITLED   DOCUMENT - Google Docs"},
            {"url": "https://docs.google.com/document/d/draft/edit", "title": "Finished plan - Google Docs"},
            {"url": "https://docs.google.com/document/d/other/edit", "title": "Copy of Untitled document - Google Docs"},
        ]
        count, docs = chrome.google_documents(rows)
        self.assertEqual(count, 4)
        self.assertEqual([doc["file_id"] for doc in docs], ["draft"])
        self.assertEqual(docs[0]["title"], "Finished plan")
