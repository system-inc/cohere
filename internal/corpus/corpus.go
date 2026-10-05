// Package corpus names the code outside this repository that some tests read, and is the only way a test
// finds it (#sycrdr6).
//
// cohere is public, and a test that reads a path on one developer's machine skips everywhere else, so its
// coverage existed only on that machine and nobody could tell. A test asks for a corpus by name instead.
// The corpus's variable says where it is. When the variable is unset the test skips with a line naming it,
// and `cohere-dev test` ends every run by naming the corpora it did not cover, so a green run says what it
// left out. No test names a path: internal/testpolicy fails one that names a home directory.
//
// `cohere-dev test` sets each unset variable from the user's corpora file, so a developer who has the
// checkouts writes the file once and every gate covers them, without exporting anything.
package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Corpus is code outside this repository that tests read.
type Corpus struct {
	// Name is how the corpora file names it.
	Name string
	// Variable is the environment variable that holds its path.
	Variable string
	// What says what the path is, in the skip line.
	What string
}

var (
	// Ahra is a checkout of ahra, which is private: its CohereSettings.json and the chain it extends, its
	// design system's stylesheets, and its installed packages.
	Ahra = Corpus{Name: "ahra", Variable: "COHERE_CORPUS_AHRA", What: "a checkout of ahra (private)"}
	// Structure is a checkout of Structure, which is private, for the TypeScript under its source/.
	Structure = Corpus{Name: "structure", Variable: "COHERE_CORPUS_STRUCTURE", What: "a checkout of Structure (private)"}
	// Connected is a checkout of www-connected-app, which is private, for its design system.
	Connected = Corpus{Name: "connected", Variable: "COHERE_CORPUS_CONNECTED", What: "a checkout of www-connected-app (private)"}
	// PrettierFork is a checkout of the Prettier fork cohere's formatter is held to, with its node_modules
	// installed, for the oracles that run its parsers.
	PrettierFork = Corpus{Name: "prettier-fork", Variable: "COHERE_PRETTIER_FORK", What: "a checkout of the Prettier fork, with node_modules"}
)

// All is every corpus, in the order the gate names them.
var All = []Corpus{Ahra, Structure, Connected, PrettierFork}

// Root is the corpus's directory, or a skip naming the variable to set when it is unset. A variable set to
// something that is not a directory fails the test: the developer asked for coverage they are not getting.
func (corpus Corpus) Root(t testing.TB) string {
	t.Helper()
	root := os.Getenv(corpus.Variable)
	if root == "" {
		t.Skipf("%s: set %s to %s to cover this test", corpus.Name, corpus.Variable, corpus.What)
	}
	information, err := os.Stat(root)
	if err != nil || !information.IsDir() {
		t.Fatalf("%s is set to %s, which is not a directory, so this test cannot read %s", corpus.Variable, root, corpus.What)
	}
	return root
}

// Path is a path inside the corpus, or the skip or failure Root gives.
func (corpus Corpus) Path(t testing.TB, elements ...string) string {
	t.Helper()
	return filepath.Join(append([]string{corpus.Root(t)}, elements...)...)
}

// Uncovered is every corpus whose variable lookup leaves unset, in All's order.
func Uncovered(lookup func(string) string) []Corpus {
	uncovered := []Corpus{}
	for _, corpus := range All {
		if lookup(corpus.Variable) == "" {
			uncovered = append(uncovered, corpus)
		}
	}
	return uncovered
}

// ConfigVariable names a corpora file to read in place of the default one.
const ConfigVariable = "COHERE_CORPORA_CONFIG"

// ConfigPath is the user's corpora file: ConfigVariable when it is set, and otherwise
// ~/.config/cohere/corpora.json.
func ConfigPath() (string, error) {
	if path := os.Getenv(ConfigVariable); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cohere", "corpora.json"), nil
}

// ReadConfig reads a corpora file, a JSON object from corpus name to absolute path:
//
//	{ "ahra": "/path/to/ahra", "structure": "/path/to/ahra/libraries/structure" }
//
// A file that does not exist is no corpora. A name no corpus has, or a relative path, is refused, so a
// typo cannot quietly leave a corpus uncovered.
func ReadConfig(path string) (map[string]string, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	if err := json.Unmarshal(contents, &paths); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	known := map[string]bool{}
	for _, corpus := range All {
		known[corpus.Name] = true
	}
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !known[name] {
			return nil, fmt.Errorf("%s names %q, which is no corpus; the corpora are %s", path, name, corpusNames())
		}
		if !filepath.IsAbs(paths[name]) {
			return nil, fmt.Errorf("%s gives %q the relative path %s; give it absolutely", path, name, paths[name])
		}
	}
	return paths, nil
}

// Environment is the variables to add to a test run's environment: each corpus the config names whose
// variable lookup leaves unset. A variable already set wins, so one run can point a corpus elsewhere.
func Environment(config map[string]string, lookup func(string) string) []string {
	environment := []string{}
	for _, corpus := range All {
		if path, found := config[corpus.Name]; found && lookup(corpus.Variable) == "" {
			environment = append(environment, corpus.Variable+"="+path)
		}
	}
	return environment
}

func corpusNames() string {
	names := make([]string, 0, len(All))
	for _, corpus := range All {
		names = append(names, corpus.Name)
	}
	return strings.Join(names, ", ")
}
