"""Run from cohere: npm exec --yes --package=typescript@6.0.3 -c 'python3 internal/lint/rules/adamic/testdata/void-widening/probe.py'."""
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parent
accepted = failed = clean = 0
with tempfile.TemporaryDirectory(prefix="cohere-void-probes-") as scratch:
    for source in sorted(root.glob("*.ts")):
        output = Path(scratch) / source.stem
        command = ["tsc", "--ignoreConfig", "--strict", "--exactOptionalPropertyTypes",
                   "--noUncheckedIndexedAccess", "--noImplicitReturns", "--target", "es2024",
                   "--module", "commonjs", "--outDir", str(output), str(source)]
        check = subprocess.run(command, capture_output=True, text=True)
        assert check.returncode == 0, (source.name, check.stdout, check.stderr)
        accepted += 1
        runtime = subprocess.run(["node", str(output / (source.stem + ".js"))], capture_output=True, text=True)
        if source.name.startswith("legal-"):
            assert runtime.returncode == 0, (source.name, runtime.stderr)
            clean += 1
        else:
            assert runtime.returncode != 0, (source.name, "runtime hole did not fail")
            assert "TypeError" in runtime.stderr or "Error: expected" in runtime.stderr, runtime.stderr
            failed += 1
        print(f"{source.name}: tsc={check.returncode}, node={runtime.returncode}", flush=True)
    excluded = Path(scratch) / "empty-object.ts"
    excluded.write_text("const make: () => void = () => {};\nconst wide: () => {} = make;\n")
    check = subprocess.run(["tsc", "--ignoreConfig", "--strict", "--noEmit", str(excluded)], capture_output=True, text=True)
    assert check.returncode != 0 and "TS2322" in check.stdout, check.stdout + check.stderr
    print("direct {}: strict tsc rejects (TS2322); Node not run")
assert (accepted, failed, clean) == (32, 19, 13), (accepted, failed, clean)
print(f"strict tsc accepted {accepted}; Node failures {failed}; Node successes {clean}")
