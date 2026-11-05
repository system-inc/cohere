package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestABareRunFormats is Kirk's ruling through the binary (#b1sjy7b, 2026-10-04: "running cohere should
// type check lint fix format all in one call"): a bare cohere formats, --fix formats, --no-fix reports
// formatting and exits on it, and --no-format leaves it out of all three.
func TestABareRunFormats(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	unformatted := "export const ugly   =   1;\n"
	formatted := "export const ugly = 1;\n"
	fixture := func(test *testing.T) string {
		test.Helper()
		root := test.TempDir()
		writeTree(test, root, map[string]string{
			"tsconfig.json":            fixScopeTsconfig,
			"CohereSettings.json":      "{ \"extends\": \"./NexusCohereSettings.json\", \"rules\": { \"no-debugger\": \"error\" } }\n",
			"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [\"tsconfig.json\"] } }\n",
			".gitignore":               ".cache/\n",
			"Ugly.ts":                  unformatted,
			"Debugged.ts":              "export function value(): number {\n  debugger;\n  return 1;\n}\n",
		})
		return root
	}
	ugly := func(root string) string { return filepath.Join(root, "Ugly.ts") }

	for _, arguments := range [][]string{{}, {"--fix"}} {
		test.Run("cohere "+strings.Join(arguments, " ")+" formats", func(test *testing.T) {
			test.Parallel()
			root := fixture(test)
			output, _ := runCohere(test, binary, root, arguments...)
			if got := readForTest(test, ugly(root)); got != formatted {
				test.Fatalf("the file was left unformatted:\n%q\n%s", got, output)
			}
			if got := readForTest(test, filepath.Join(root, "Debugged.ts")); strings.Contains(got, "debugger") {
				test.Fatalf("the fix did not land beside the format:\n%s", got)
			}
		})
	}

	// Formatting by default keeps a warm run replayable: once the tree is formatted, an unchanged bare run is
	// recorded and the next one replays it.
	test.Run("a warm bare run still replays", func(test *testing.T) {
		test.Parallel()
		root := fixture(test)
		runCohere(test, binary, root)
		runCohere(test, binary, root)
		output, code := runCohere(test, binary, root)
		if code != 0 || !strings.HasPrefix(output, "cached: ") {
			test.Fatalf("a warm bare run over a formatted tree did not replay, exit %d:\n%s", code, output)
		}
	})

	test.Run("cohere --no-fix reports the misformat and exits on it", func(test *testing.T) {
		test.Parallel()
		root := fixture(test)
		output, code := runCohere(test, binary, root, "--no-fix")
		if code == 0 ||
			!strings.Contains(output, ugly(root)+":1:1 - --fix would rewrite this file: format [fix/would-change]") {
			test.Fatalf("exit %d, and the misformat was not a finding:\n%s", code, output)
		}
		if got := readForTest(test, ugly(root)); got != unformatted {
			test.Fatalf("--no-fix wrote the file:\n%s", got)
		}
	})

	for _, arguments := range [][]string{{"--no-format"}, {"--fix", "--no-format"}, {"--no-fix", "--no-format"}} {
		test.Run("cohere "+strings.Join(arguments, " ")+" leaves formatting out", func(test *testing.T) {
			test.Parallel()
			root := fixture(test)
			output, _ := runCohere(test, binary, root, arguments...)
			if got := readForTest(test, ugly(root)); got != unformatted {
				test.Fatalf("--no-format formatted the file:\n%s", got)
			}
			if strings.Contains(output, ugly(root)) {
				test.Fatalf("--no-format named the misformat:\n%s", output)
			}
			if !strings.Contains(output, "(formatting left out (--no-format))") {
				test.Fatalf("the phase line does not say formatting was left out:\n%s", output)
			}
		})
	}

	test.Run("--no-format refuses a flag that asks for formatting", func(test *testing.T) {
		test.Parallel()
		root := fixture(test)
		for _, other := range []string{"--format", "--format-all", "--format-only"} {
			output, code := runCohere(test, binary, root, "--no-format", other)
			if code == 0 || !strings.Contains(output, "--no-format and "+other+" contradict each other") {
				test.Fatalf("--no-format with %s was not refused by name, exit %d:\n%s", other, code, output)
			}
		}
	})
}
