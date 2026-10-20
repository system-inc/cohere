#!/usr/bin/env python3
"""Run the #t98h9pd guard mutants through Go overlays; never edit production source."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[6]
SOURCE = ROOT / "internal/lint/rules/adamic/no_cross_enum_narrowing.go"
MUTANTS = {'unknown-member-hides-known': ('if crossEnumOwner(ctx.TypeChecker, part) != compared[0].enum {',
                                'if part.Flags()&checker.TypeFlagsEnum != 0 || '
                                'crossEnumOwner(ctx.TypeChecker, part) != compared[0].enum {'),
 'matching-member-overlap': ('if member.enum == other.enum && member.value == other.value {',
                             'if member.enum == other.enum {'),
 'shared-value': ('member.enum == other.enum || member.value != other.value',
                  'member.enum == other.enum'),
 'different-enum': ('member.enum == other.enum || member.value != other.value',
                    'member.value != other.value'),
 'overlap': ('if !shared {', 'if !shared && false {'),
 'single-enum': ('if !singleEnum {', 'if !singleEnum && false {'),
 'declared-type': ('crossEnumDeclaredType(ctx.TypeChecker, sides[1])',
                   'ctx.TypeChecker.GetTypeAtLocation(sides[1])'),
 'strict-equal': ('case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, '
                  'ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:',
                  'case ast.KindExclamationEqualsEqualsToken, ast.KindEqualsEqualsToken, '
                  'ast.KindExclamationEqualsToken:'),
 'strict-unequal': ('case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, '
                    'ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:',
                    'case ast.KindEqualsEqualsEqualsToken, ast.KindEqualsEqualsToken, '
                    'ast.KindExclamationEqualsToken:'),
 'loose-equal': ('case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, '
                 'ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:',
                 'case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, '
                 'ast.KindExclamationEqualsToken:'),
 'loose-unequal': ('case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, '
                   'ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:',
                   'case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, '
                   'ast.KindEqualsEqualsToken:'),
 'switch': ('ast.KindCaseClause:', 'ast.KindDefaultClause:'),
 'swapped-side': ('{{left, right}, {right, left}}', '{{left, right}}')}

if "--list" in sys.argv:
    print("\n".join(MUTANTS))
    raise SystemExit(0)

original = SOURCE.read_text()
with tempfile.TemporaryDirectory(prefix=".cross-enum-mutants-", dir=ROOT) as directory:
    scratch = Path(directory)
    for name, (old, new) in MUTANTS.items():
        if old not in original:
            raise SystemExit(f"{name}: source guard moved; update this mutant")
        variant = scratch / f"{name}.go"
        variant.write_text(original.replace(old, new, 1))
        overlay = scratch / f"{name}.json"
        overlay.write_text(json.dumps({"Replace": {str(SOURCE): str(variant)}}))
        result = subprocess.run(
            ["go", "test", f"-overlay={overlay}", "./internal/lint/rules/adamic",
             "-run", "^(TestCrossEnumNarrowing|TestWholeComputedEnumNarrowing)", "-v"],
            cwd=ROOT, capture_output=True, text=True,
        )
        failures = [line.strip() for line in result.stdout.splitlines()
                    if ("--- FAIL: TestCrossEnumNarrowing" in line and "/" in line)
                    or "--- FAIL: TestWholeComputedEnumNarrowing" in line]
        if result.returncode != 1 or not failures:
            raise SystemExit(f"{name} survived or failed without an assertion:\n"
                             + result.stdout + result.stderr)
        print(f"{name}: {len(failures)} failing fixture tests; {failures[0]}", flush=True)
print(f"{len(MUTANTS)} mutants caught")
