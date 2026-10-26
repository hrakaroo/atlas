"""Export Atlas schedule definitions locally; never registers jobs."""
import argparse
import json
import os
from pathlib import Path

FIELDS = ("id", "name", "kind", "status", "rrule", "notification_policy", "model", "reasoning_effort")
CADENCES = {
    "atlas-chrome-document-import": "Every 15 minutes",
    "atlas-requested-document-refresh": "Every hour, minute 00",
    "atlas-morning-document-refresh": "Daily at 6:00 a.m. America/Los_Angeles",
    "atlas-zoom-transcript-import": "Every hour, minute 10",
    "daily-ai-briefing": "Daily at 6:00 a.m. America/Los_Angeles",
}
def describe_cadence(rrule):
    parts = dict(part.split("=", 1) for part in rrule.removeprefix("RRULE:").split(";") if "=" in part)
    interval = int(parts.get("INTERVAL", "1"))
    if parts.get("FREQ") == "HOURLY" and interval == 1 and "BYMINUTE" in parts:
        return "Every hour, minute " + parts["BYMINUTE"].zfill(2)
    if parts.get("FREQ") == "DAILY" and interval == 1 and "BYHOUR" in parts:
        return "Daily at " + parts["BYHOUR"].zfill(2) + ":" + parts.get("BYMINUTE", "0").zfill(2) + " America/Los_Angeles"
    return rrule

def read_definition(path):
    # Scheduler files use flat TOML with JSON-compatible quoted strings.
    # Parse only the fields we export. Fail on unsupported encodings rather
    # than silently lose prompt text or rewrite scheduler-owned files.
    result = {}
    for line in path.read_text().splitlines():
        if " = " not in line:
            continue
        key, value = line.split(" = ", 1)
        if key in FIELDS + ("prompt", "target_thread_id"):
            result[key] = json.loads(value)
    for key in ("id", "name", "kind", "status", "rrule", "prompt"):
        if key not in result:
            raise ValueError("Missing " + key + " in " + str(path))
    return result

def export(source, destination, atlas_root, home):
    destination.mkdir(parents=True, exist_ok=True)
    parameters = {"ATLAS_ROOT": str(atlas_root), "ZOOM_ROOT": str(home / "Documents/Zoom"),
                  "OBSIDIAN_ROOT": str(home / "Documents/Obsidian"), "HOME": str(home)}
    records = []
    for path in sorted(source.glob("*/automation.toml")):
        record = read_definition(path)
        if record["id"] not in CADENCES and not record["id"].startswith("atlas-"):
            continue
        ident = record["id"]
        prompt = record.pop("prompt")
        for name, value in parameters.items():
            prompt = prompt.replace(value, "${" + name + "}")
        prompt_file = ident + ".md"
        (destination / prompt_file).write_text(prompt + "\n")
        # Chat IDs are local bindings, not portable definitions.
        record.pop("target_thread_id", None)
        record.update(prompt_file=prompt_file, cadence=CADENCES.get(ident, describe_cadence(record["rrule"])),
                      timezone="America/Los_Angeles",
                      destination_chat="Daily AI briefing" if ident=="daily-ai-briefing" else "Atlas scheduled checks")
        records.append(record)
    manifest = {"schema_version": 1, "parameters": parameters,
                "registration": "Use the desktop automation tools to create or update jobs; these files do not register jobs automatically.",
                "schedules": records}
    (destination / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    return records

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=Path(os.environ.get("CODEX_HOME", str(Path.home()/".codex"))) / "automations")
    parser.add_argument("--destination", type=Path, default=Path(__file__).resolve().parents[1]/"schedules")
    args = parser.parse_args()
    records = export(args.source, args.destination, Path(__file__).resolve().parents[1], Path.home())
    print(json.dumps({"exported": [record["id"] for record in records], "destination": str(args.destination)}))

if __name__ == "__main__":
    main()
