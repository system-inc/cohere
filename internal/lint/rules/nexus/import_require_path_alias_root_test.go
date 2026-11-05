package nexus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
)

/*
 * The root is settled when the options are decoded, so the rule is never handed one that matches
 * nothing.
 *
 * A committed absolute root is right on one machine in one checkout. Everywhere else no file is
 * under it, the rule declines every file, and the silence reads exactly like a tree with no deep
 * imports. ahra pinned `/Users/kirkouimet/Projects/ahra` and the rule went dark in every worktree.
 *
 * Each accepting case asserts the root the rule will actually use, and each refusing case asserts
 * the error names repositoryRoot, so a decoder that refused everything, or accepted everything as
 * written, fails here.
 */

const rootTestAliases = `"aliases": [{"directory": ".", "alias": "@project"}]`

func decodeRootForTest(test *testing.T, root string, base rule.OptionsBase) (ImportRequirePathAliasOptions, error) {
	test.Helper()
	raw := "{" + rootTestAliases + "}"
	if root != "" {
		raw = "{" + rootTestAliases + `, "repositoryRoot": "` + root + `"}`
	}
	decoded, decodeImportRequirePathAliasOptionsError := decodeImportRequirePathAliasOptions([]byte(raw), base)
	if decodeImportRequirePathAliasOptionsError != nil {
		return ImportRequirePathAliasOptions{}, decodeImportRequirePathAliasOptionsError
	}
	return decoded.(ImportRequirePathAliasOptions), nil
}

func TestImportRequirePathAliasRootIsAnchored(test *testing.T) {
	test.Parallel()

	// The config sits one level above the project, as a repository-level config over a monorepo
	// project would, so "relative to the config" and "the project root" are different answers and a
	// decoder that confused them fails a case below.
	configDirectory := test.TempDir()
	projectRoot := filepath.Join(configDirectory, "project")
	for _, directory := range []string{projectRoot, filepath.Join(configDirectory, "beside")} {
		if createDirectoryError := os.Mkdir(directory, 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
	}
	base := rule.OptionsBase{ConfigDirectory: configDirectory, ProjectRoot: projectRoot}

	accepted := []struct {
		name string
		root string
		want string
	}{
		// The spelling ahra should commit: the config's own directory, on every machine.
		{"dot is the config's directory", ".", configDirectory},
		// Relative to the config file, not to the project root and not to the process.
		{"a relative path resolves against the config", "project", projectRoot},
		{"absent defaults to the project root", "", projectRoot},
		// An absolute root that holds the project is still honoured, so a config written before this
		// keeps working on the machine it was written on.
		{"an absolute root holding the project is kept", configDirectory, configDirectory},
	}
	for _, testCase := range accepted {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeRootForTestError := decodeRootForTest(test, testCase.root, base)
			if decodeRootForTestError != nil {
				test.Fatalf("refused: %v", decodeRootForTestError)
			}
			if decoded.RepositoryRoot != testCase.want {
				test.Errorf("root = %q, want %q", decoded.RepositoryRoot, testCase.want)
			}
			if len(decoded.Aliases) != 1 {
				test.Errorf("the aliases did not survive decoding: %+v", decoded.Aliases)
			}
		})
	}

	aFile := filepath.Join(configDirectory, "NotADirectory.json")
	if writeError := os.WriteFile(aFile, []byte("{}"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	refused := []struct {
		name string
		root string
		base rule.OptionsBase
	}{
		// The pinned path from another machine: the exact shape that made the rule inert.
		{"an absolute root that does not exist here", filepath.Join(configDirectory, "elsewhere", "ahra"), base},
		{"a relative root that resolves to nothing", "missing", base},
		{"a root that is a file", "NotADirectory.json", base},
		// The worktree case: the pinned root exists, and it is another checkout, so nothing this run
		// checks is under it.
		{"an existing absolute root beside the project", test.TempDir(), base},
		{"a relative root beside the project", "beside", base},
		{"absent with no project root known", "", rule.OptionsBase{ConfigDirectory: configDirectory}},
		{"relative with no config directory known", ".", rule.OptionsBase{ProjectRoot: projectRoot}},
	}
	for _, testCase := range refused {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeRootForTestError := decodeRootForTest(test, testCase.root, testCase.base)
			if decodeRootForTestError == nil {
				test.Fatalf("accepted a root that cannot be determined: %q", decoded.RepositoryRoot)
			}
			if !strings.Contains(decodeRootForTestError.Error(), "repositoryRoot") {
				test.Errorf("the refusal does not name the option: %v", decodeRootForTestError)
			}
		})
	}
}
