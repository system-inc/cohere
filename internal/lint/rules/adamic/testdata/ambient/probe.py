#!/usr/bin/env python3
"""Run with: python3 probe.py /absolute/path/to/typescript/bin/tsc.

Requires TypeScript 6.0.3 and Node 24 on PATH. Each directory is the same
multi-file program used by the Go fixtures. No npm packages are needed at runtime.
"""
from pathlib import Path
import json
import shutil
import subprocess
import sys
import tempfile

compiler = str(Path(sys.argv[1]).resolve())
root = Path(__file__).resolve().parent
failures = 0
for fixture in sorted(path for path in root.iterdir() if path.is_dir()):
    with tempfile.TemporaryDirectory(prefix="ambient-probe-", dir=Path.cwd()) as temporary:
        directory = Path(temporary)
        shutil.copytree(fixture, directory, dirs_exist_ok=True)
        (directory / "package.json").write_text('{"type":"module"}')
        options = {
            "strict": True,
            "noUncheckedIndexedAccess": True,
            "exactOptionalPropertyTypes": True,
            "noImplicitReturns": True,
            "noFallthroughCasesInSwitch": True,
            "erasableSyntaxOnly": True,
            "verbatimModuleSyntax": True,
            "module": "nodenext",
            "target": "es2024",
            "lib": ["es2024"],
            "types": [],
            "outDir": "out",
        }
        (directory / "tsconfig.json").write_text(json.dumps({"compilerOptions": options, "include": ["*.ts"]}))
        check = subprocess.run(["node", compiler, "-p", str(directory)], capture_output=True, text=True)
        if check.returncode:
            print(f"{fixture.name}: tsc={check.returncode}; {check.stdout.strip()}")
            failures += 1
            continue
        run = subprocess.run(["node", str(directory / "out/Case.js")], capture_output=True, text=True)
        reason = next((line for line in run.stderr.splitlines() if line.startswith(("ReferenceError:", "TypeError:"))), "")
        expected = 0 if fixture.name == "accepted" else 1
        failures += int(run.returncode != expected or (expected == 1 and not reason))
        print(f"{fixture.name}: tsc=0, node={run.returncode}, {reason or 'completed'}")
sys.exit(bool(failures))
