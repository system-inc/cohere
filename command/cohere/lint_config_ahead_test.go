package main

import (
	"strings"
	"testing"
)

// A project that names its own sets has its config read beside the graph build (lintConfigAhead), and a config it
// cannot run with still stops the run, with the error a config read after the build gives. Settings no rule reads
// fail the settings check. An override that reaches no file fails the selectors, which wait for the program, and
// it is the error reported even when the settings are bad too, since the selectors were always checked first.
func TestAConfigReadAheadOfTheGraphRefusesWhatItAlwaysRefused(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	project := func(test *testing.T, settings string) string {
		test.Helper()
		root := test.TempDir()
		writeTree(test, root, map[string]string{
			"tsconfig.json":       `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler"},"include":["source"]}`,
			"source/A.ts":         "export const a = 1;\n",
			"CohereSettings.json": settings,
		})
		return root
	}
	const vacuous = `"overrides": [{"files": ["nowhere/**"], "rules": {"no-debugger": "off"}}]`
	cases := []struct {
		name, settings, refusal, absent string
	}{
		{"settings no rule reads", `{"extends": ["cohere:react"], "settings": {"nobody-reads-this": {"a": 1}}}`,
			`Validating the lint config: settings names "nobody-reads-this", which no rule cohere runs reads`, ""},
		{
			"an override reaching no file, before bad settings",
			`{"extends": ["cohere:react"], ` + vacuous + `, "settings": {"nobody-reads-this": {"a": 1}}}`,
			`Validating the lint config: lint config declares override 1 pattern "nowhere/**", which matched none`,
			"nobody-reads-this",
		},
	}
	for _, refused := range cases {
		test.Run(refused.name, func(test *testing.T) {
			test.Parallel()
			output, exitCode := runCohere(
				test,
				binary,
				project(test, refused.settings),
				"--no-fix",
				"--no-cache",
			)
			if exitCode == 0 || !strings.Contains(output, refused.refusal) {
				test.Fatalf("the run was not refused with %q (exit %d):\n%s", refused.refusal, exitCode, output)
			}
			if refused.absent != "" && strings.Contains(output, refused.absent) {
				test.Fatalf(
					"the run reported %q, which is checked after the error it should have stopped at:\n%s",
					refused.absent,
					output,
				)
			}
		})
	}
	// The control: the same project with a config it can run with is checked, so the refusals above are about the
	// config and not the project. It has findings of its own, so what it must not do is refuse.
	output, _ := runCohere(
		test,
		binary,
		project(test, `{"extends": ["cohere:react"]}`),
		"--no-fix",
		"--no-cache",
	)
	if strings.Contains(output, "the lint config") || !strings.Contains(output, "lint: ") {
		test.Fatalf("the control project's config was refused, or it never reached lint:\n%s", output)
	}
}
