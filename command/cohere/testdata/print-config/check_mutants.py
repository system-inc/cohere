#!/usr/bin/env python3
"""Run print-config's negative controls from the cohere root; restore source after each run."""
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[4]
source = root / "command/cohere/print_config.go"
mutants = [
    (
        "drop settings section",
        '`json:"settings"`',
        '`json:"-"`',
        "TestPrintConfigExplainsAFile",
        "missing section settings",
    ),
    (
        "drop settings readers",
        "settings := map[string]json.RawMessage{}\n\tfor _, registration := range registrations",
        "settings := map[string]json.RawMessage{}\n\tregistrations = nil\n\tfor _, registration := range registrations",
        "TestPrintConfigSettingsReaders",
        "registered settings = map[]",
    ),
]
for name, before, after, test, diagnostic in mutants:
    original = source.read_bytes()
    text = original.decode()
    if text.count(before) != 1:
        raise SystemExit(f"{name}: expected exactly one mutation site")
    command = ["go", "test", "./command/cohere", "-run", f"^{test}$", "-v"]
    try:
        source.write_text(text.replace(before, after, 1))
        result = subprocess.run(command, cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        print(f"CONTROL: {name}\nCOMMAND: {' '.join(command)}\n{result.stdout}", flush=True)
        if result.returncode != 1 or f"--- FAIL: {test}" not in result.stdout or diagnostic not in result.stdout:
            raise SystemExit(f"{name}: test did not catch the intended defect")
    finally:
        source.write_bytes(original)
print("Controls caught: 2/2; original source restored.")
