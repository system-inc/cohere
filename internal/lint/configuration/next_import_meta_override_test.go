package configuration

import (
	"path/filepath"
	"testing"
)

// cohere:next reports a module-scope import.meta path everywhere a Next server bundle could import the
// module, and nowhere Node or the test runner runs the file directly: a test file, or an ESLint config
// (#ay7a8vx). Read from the real embedded set, so the set file itself is what is pinned, both ways.
func TestNextLeavesImportMetaPathsToTestsAndEslintConfigs(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"CohereSettings.json": `{"extends": "cohere:next"}`,
	})
	loaded, err := Load(filepath.Join(directory, "CohereSettings.json"))
	if err != nil {
		test.Fatal(err)
	}
	const name = "nexus/correctness-no-load-time-import-meta-path"
	for _, file := range []string{"source/RunSuites.ts", "app/page.tsx", "modules/os/Paths.ts"} {
		if !loaded.Resolve(filepath.Join(directory, file)).Enabled(name) {
			test.Errorf("%s: %s should run in an ordinary module", file, name)
		}
	}
	for _, file := range []string{"command-line/StructureProcess.test.ts", "source/system/StandardStreams.node.test.ts", "eslint.config.mjs", "eslint.config.ts"} {
		if loaded.Resolve(filepath.Join(directory, file)).Enabled(name) {
			test.Errorf("%s: %s should be off, since Node runs the file directly", file, name)
		}
	}
}
