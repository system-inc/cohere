# Print-config fixture and controls

`TestPrintConfigExplainsAFile` builds a temporary project containing Tailwind settings,
a global declaration, a top-level departure, and two overrides. It checks the JSON
sections, their order and values, both module and script source types, the five
nexus aliases, and refusal of unread React settings. Turning Tailwind off in the
second file removes its settings from that file's answer.

Settings are the configured values consumed by enabled registered readers. Their
paths retain their configured spelling, relative to the lint configuration directory;
rule-specific options remain in `rules`. Language options come from the actual
TypeScript program: its target, the selected tsconfig, the file's source type, and
global value names mapped to their declaration files. No ESLint parser identity or
plugin package version is emitted. A registered/configured `unused-disable` travels
through the ordinary rule loop; a binary without that rule invents no setting.

`TestPrintConfigSettingsReaders` supplies an isolated future plugin registration,
so another plugin needs no print-config change. It also checks that off rules and
unregistered namespaces contribute nothing.

From the cohere root, with the setup environment sourced:

```sh
go test ./command/cohere -run '^TestPrintConfig' -v
python3 command/cohere/testdata/print-config/check_mutants.py
```

The mutant runner temporarily changes `print_config.go` and restores the original
bytes in `finally`; run it without concurrent builds. Dropping the `settings` JSON
section must fail `TestPrintConfigExplainsAFile` with `missing section settings`.
Dropping the settings readers must fail `TestPrintConfigSettingsReaders` with
`registered settings = map[]`. Each mutant is checked for the intended test failure,
so a build error does not count as a caught defect.

Before the implementation, the project fixture ran against 06af2e399c6c and failed:
all five additional sections and all five nexus aliases were absent, help lacked
the ESLint-only fields, and print-config accepted React settings the lint run refuses.
After implementation, the three print-config tests pass, and both mutants are caught.

## Validation on the cloud worker

Counts below include named subtests. No whole-module test run was used.

- `go test ./command/cohere -run '^TestPrintConfig' -v`: 3 passed.
- `python3 command/cohere/testdata/print-config/check_mutants.py`: 2/2 caught;
  each selected test failed once with its intended assertion, and source was restored.
- `go test -json ./internal/lint/configuration`: 130 passed, 4 skipped, 0 failed.
- `go test -json ./internal/testpolicy ./internal/lint/registry ./internal/guard`:
  testpolicy 4 passed; registry 26,116 passed and 2 skipped; guard 33 passed;
  0 failures across all three packages.
- `go test -json ./command/cohere`: 463 passed, 12 failed, 0 skipped.
- The command below, against unchanged 06af2e399c6c, produced 7 passes and the same
  12 failing test events. Comparing failure names found 0 changed-only and 0
  baseline-only failures. These cover Swift platform selection, child process cleanup,
  held-output detection, cache discovery and zero-config Tailwind package detection.

```sh
go test -json ./command/cohere -run '^(TestADevelopmentCohereIgnoresAnEngineBesideIt|TestAFileCreatedAfterDiscoveryIsFormattedNextRun|TestAFileCreatedAfterDiscoveryIsNeverReplayedOver|TestAHeldOutputNamesItsHolder|TestAReleasedCohereRunsTheEngineShippedBesideIt|TestAReleasedCohereWithNoEngineBesideItNeverBuildsOne|TestATerminatedMixedRunLeavesNoChildBehind|TestRunEngineBinaryEndings|TestZeroConfigReadsTailwindSettingsOrRefusesThem)$'
```

`go build ./command/cohere` exits 0. `go vet ./command/cohere
./internal/lint/configuration` reports 0 diagnostics. `gofmt -l` on main.go,
print_config.go, print_config_test.go and configuration/resolve.go lists 0 files;
`git diff --check` reports 0 whitespace errors.

The final origin/main check still pointed at 06af2e399c6c: no registered
`unused-disable` exists there. No dead-directive setting is fabricated. No rules
were added or renamed, so rule-list generation and corpus gap changes do not apply.
