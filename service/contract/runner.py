"""Run endpoint contracts against one selected isolated service implementation."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import sys
import unittest
import harness


class EvidenceResult(unittest.TextTestResult):
    """Retain individual executed outcomes after unittest consumes the suite."""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.outcomes = []

    def addSuccess(self, test):
        super().addSuccess(test)
        self.outcomes.append({"test": test.id(), "status": "pass"})

    def addFailure(self, test, err):
        super().addFailure(test, err)
        self.outcomes.append({"test": test.id(), "status": "fail"})

    def addError(self, test, err):
        super().addError(test, err)
        self.outcomes.append({"test": test.id(), "status": "error"})

    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        self.outcomes.append({"test": test.id(), "status": "skip", "reason": reason})

    def addExpectedFailure(self, test, err):
        super().addExpectedFailure(test, err)
        self.outcomes.append({"test": test.id(), "status": "expected_failure"})

    def addUnexpectedSuccess(self, test):
        super().addUnexpectedSuccess(test)
        self.outcomes.append({"test": test.id(), "status": "unexpected_success"})

    def addSubTest(self, test, subtest, err):
        super().addSubTest(test, subtest, err)
        if err is not None:
            status = "fail" if issubclass(err[0], test.failureException) else "error"
            self.outcomes.append({"test": test.id(), "subtest": subtest.id(), "status": status})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--backend", choices=("go", "python", "command"), default="go")
    parser.add_argument("--command", help="JSON argv for the command backend; library/port arguments are appended")
    selection = parser.add_mutually_exclusive_group()
    selection.add_argument("--pattern", default=None)
    selection.add_argument("--profile", choices=("gate3",), help="Named explicit acceptance subset; full discovery remains the default")
    parser.add_argument("--capture", action="store_true", help="Capture synthetic response diagnostics; not normalized golden output")
    args = parser.parse_args()
    if args.capture:
        os.environ["ATLAS_CONTRACT_CAPTURE"] = "1"
    else:
        os.environ.pop("ATLAS_CONTRACT_CAPTURE", None)
    if args.backend == "go":
        binary = harness.ROOT / "service" / ".work" / "atlas-service"
        if not binary.is_file():
            parser.error("Build service/.work/atlas-service first; see service/contract/README.md")
        os.environ["ATLAS_CONTRACT_COMMAND"] = json.dumps([str(binary), "--contract-mode", "--owner-name", "John Smith", "--timezone", "America/Los_Angeles"])
    elif args.backend == "command":
        raw = args.command or os.environ.get("ATLAS_CONTRACT_COMMAND")
        if not raw:
            parser.error("--command or ATLAS_CONTRACT_COMMAND is required")
        command = json.loads(raw)
        if not isinstance(command, list) or not command or not all(isinstance(x, str) for x in command):
            parser.error("command must be a nonempty JSON string array")
        os.environ["ATLAS_CONTRACT_COMMAND"] = json.dumps(command)
    else:
        launcher = harness.ROOT / "old" / "contract" / "python_backend.py"
        if not launcher.is_file():
            parser.error("Python reference is an optional local old/ archive, excluded from Git; use --backend go")
        os.environ["ATLAS_CONTRACT_COMMAND"] = json.dumps([sys.executable, str(launcher)])
    selected_ids = None
    if args.profile:
        profiles = json.loads((Path(__file__).parent / "profiles.json").read_text())
        selected_ids = profiles[args.profile]["test_ids"]
        if not selected_ids or len(selected_ids) != len(set(selected_ids)):
            parser.error("Profile requires distinct explicit test IDs")
        suite = unittest.defaultTestLoader.loadTestsFromNames(selected_ids)
    else:
        suite = unittest.defaultTestLoader.discover(str(Path(__file__).parent), pattern=args.pattern or "test_*.py")
    result = unittest.TextTestRunner(verbosity=2, resultclass=EvidenceResult).run(suite)
    folder = harness.ROOT / "service" / ".work" / "reports"
    folder.mkdir(parents=True, exist_ok=True)
    successful = (result.testsRun > 0 and result.wasSuccessful() and not result.skipped
                  and all(entry["status"] == "pass" for entry in result.outcomes))
    report = {
        "backend": args.backend, "profile": args.profile, "selected_test_ids": selected_ids, "tests_run": result.testsRun,
        "test_outcomes": result.outcomes,
        "failures": [name.id() for name, _ in result.failures],
        "errors": [name.id() for name, _ in result.errors],
        "skipped": [{"test": name.id(), "reason": why} for name, why in result.skipped],
        "passed": successful, "observed_requests": harness.OBSERVED,
        "scope": "Only executed assertions; observed request catalog is not full coverage",
        "diagnostic_captures": harness.CAPTURES,
    }
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    path = folder / (args.backend + "-" + stamp + ".json")
    path.write_text(json.dumps(report, indent=2) + "\n")
    print("Contract report:", path)
    if result.testsRun == 0:
        print("Contract acceptance failed: no tests discovered.")
    return 0 if successful else 1


if __name__ == "__main__":
    sys.exit(main())
