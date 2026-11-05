package corpus

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func lookupFrom(variables map[string]string) func(string) string {
	return func(name string) string { return variables[name] }
}

// Every corpus has a name, a variable and a description, and no two share a name or a variable, so the
// config and the gate's line cannot confuse them.
func TestEveryCorpusIsNamedOnce(test *testing.T) {
	test.Parallel()
	names, variables := map[string]bool{}, map[string]bool{}
	for _, corpus := range All {
		if corpus.Name == "" || corpus.Variable == "" || corpus.What == "" {
			test.Errorf("%+v is missing a name, variable or description", corpus)
		}
		if names[corpus.Name] || variables[corpus.Variable] {
			test.Errorf("%s or %s is used twice", corpus.Name, corpus.Variable)
		}
		names[corpus.Name], variables[corpus.Variable] = true, true
	}
}

func TestUncoveredIsEveryUnsetCorpus(test *testing.T) {
	test.Parallel()
	got := Uncovered(lookupFrom(map[string]string{Ahra.Variable: "/somewhere"}))
	want := []Corpus{Structure, Connected, PrettierFork}
	if !reflect.DeepEqual(got, want) {
		test.Errorf("uncovered %v, want %v", got, want)
	}
	if got := Uncovered(lookupFrom(nil)); len(got) != len(All) {
		test.Errorf("with nothing set, %d uncovered, want all %d", len(got), len(All))
	}
}

func writeConfig(test *testing.T, contents string) string {
	test.Helper()
	path := filepath.Join(test.TempDir(), "corpora.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		test.Fatal(err)
	}
	return path
}

// A config fills only what the environment leaves unset, so a run can still point one corpus elsewhere.
func TestEnvironmentFillsOnlyUnsetVariables(test *testing.T) {
	test.Parallel()
	config, err := ReadConfig(writeConfig(test, `{"ahra": "/checkouts/ahra", "structure": "/checkouts/structure"}`))
	if err != nil {
		test.Fatal(err)
	}
	got := Environment(config, lookupFrom(map[string]string{Structure.Variable: "/elsewhere"}))
	if want := []string{"COHERE_CORPUS_AHRA=/checkouts/ahra"}; !reflect.DeepEqual(got, want) {
		test.Errorf("environment %v, want %v", got, want)
	}
}

// A missing file is no corpora; a typo or a relative path is refused rather than leaving a corpus
// quietly uncovered.
func TestReadConfigRefusesWhatWouldLoseACorpus(test *testing.T) {
	test.Parallel()
	if config, err := ReadConfig(filepath.Join(test.TempDir(), "absent.json")); err != nil || len(config) != 0 {
		test.Errorf("a missing file read as %v, %v", config, err)
	}
	for name, contents := range map[string]string{
		"a name no corpus has": `{"arha": "/checkouts/ahra"}`,
		"a relative path":      `{"ahra": "checkouts/ahra"}`,
		"not an object":        `["/checkouts/ahra"]`,
	} {
		if _, err := ReadConfig(writeConfig(test, contents)); err == nil {
			test.Errorf("%s was read", name)
		}
	}
}

// Root skips naming the variable when it is unset, and fails when it is set to something that is not a
// directory. Each is run in a child process, since a skip and a failure end the test that meets them.
func TestRootSkipsWhenUnsetAndFailsWhenWrong(test *testing.T) {
	test.Parallel()
	if probe := os.Getenv("COHERE_CORPUS_PROBE"); probe != "" {
		Ahra.Root(test)
		return
	}
	for name, testCase := range map[string]struct {
		value string
		want  string
	}{
		"unset":           {value: "", want: "--- SKIP"},
		"not a directory": {value: filepath.Join(test.TempDir(), "absent"), want: "--- FAIL"},
		"a directory":     {value: test.TempDir(), want: "--- PASS"},
	} {
		output := runProbe(test, "TestRootSkipsWhenUnsetAndFailsWhenWrong", "1", testCase.value)
		if !strings.Contains(output, testCase.want) {
			test.Errorf("%s: want %s in:\n%s", name, testCase.want, output)
		}
		if testCase.want == "--- SKIP" && !strings.Contains(output, Ahra.Variable) {
			test.Errorf("the skip does not name %s:\n%s", Ahra.Variable, output)
		}
	}
}

// A spelling is a corpus name, a colon and the path inside it. A name no corpus has is refused rather than
// read as a relative path, and an absolute path, or a one-letter drive, is no spelling at all.
func TestSpelledSplitsOnlyCorpusSpellings(test *testing.T) {
	test.Parallel()
	for spelling, want := range map[string]struct {
		corpus  Corpus
		inside  string
		spelled bool
	}{
		"ahra:app/_theme/styles/theme.css": {corpus: Ahra, inside: "app/_theme/styles/theme.css", spelled: true},
		"connected:":                       {corpus: Connected, inside: "", spelled: true},
		"prettier-fork:tests/format":       {corpus: PrettierFork, inside: "tests/format", spelled: true},
		"/checkouts/ahra/theme.css":        {},
		"c:/checkouts/ahra/theme.css":      {},
		"theme.css":                        {},
	} {
		corpus, inside, spelled, err := Spelled(spelling)
		if err != nil || corpus != want.corpus || inside != want.inside || spelled != want.spelled {
			test.Errorf("Spelled(%q) = %v, %q, %v, %v; want %v, %q, %v", spelling, corpus, inside, spelled, err, want.corpus, want.inside, want.spelled)
		}
	}
	if _, _, spelled, err := Spelled("arha:app/_theme/styles/theme.css"); !spelled || err == nil || !strings.Contains(err.Error(), "no corpus") {
		test.Errorf("a misspelled corpus read as %v, %v; want it refused", spelled, err)
	}
}

// Resolve skips naming the variable when the corpus is unset, fails when it is set and the path is not in
// it, and fails on a spelling that names no corpus whether the corpus is set or not. A path inside this
// repository resolves with no corpus set, and an absolute path, or a relative one that climbs out of the
// repository, fails. Each runs in a child process, since a skip and a failure end the test that meets
// them.
func TestResolveSkipsWhenUnsetAndFailsWhenWrong(test *testing.T) {
	test.Parallel()
	if probe := os.Getenv("COHERE_CORPUS_PROBE"); probe != "" {
		if path := Resolve(test, probe); !filepath.IsAbs(path) {
			test.Fatalf("Resolve(%q) = %q, which is not absolute", probe, path)
		}
		return
	}
	root := test.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app", "styles"), 0o755); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "styles", "theme.css"), nil, 0o644); err != nil {
		test.Fatal(err)
	}
	for name, testCase := range map[string]struct {
		spelling string
		value    string
		want     string
	}{
		"unset":                   {spelling: "ahra:app/styles/theme.css", want: "--- SKIP"},
		"present":                 {spelling: "ahra:app/styles/theme.css", value: root, want: "--- PASS"},
		"the root":                {spelling: "ahra:", value: root, want: "--- PASS"},
		"missing from the corpus": {spelling: "ahra:app/styles/absent.css", value: root, want: "--- FAIL"},
		"no corpus, set":          {spelling: "arha:app/styles/theme.css", value: root, want: "--- FAIL"},
		"no corpus, unset":        {spelling: "arha:app/styles/theme.css", want: "--- FAIL"},
		"an absolute path":        {spelling: filepath.Join(root, "app", "styles", "theme.css"), value: root, want: "--- FAIL"},
		"not in the repository":   {spelling: "app/styles/theme.css", want: "--- FAIL"},
		"in the repository":       {spelling: "internal/corpus/corpus.go", want: "--- PASS"},
		"out of the repository":   {spelling: "..", want: "--- FAIL"},
	} {
		output := runProbe(test, "TestResolveSkipsWhenUnsetAndFailsWhenWrong", testCase.spelling, testCase.value)
		if !strings.Contains(output, testCase.want) {
			test.Errorf("%s: want %s in:\n%s", name, testCase.want, output)
		}
		if testCase.want == "--- SKIP" && !strings.Contains(output, Ahra.Variable) {
			test.Errorf("the skip does not name %s:\n%s", Ahra.Variable, output)
		}
	}
}

// The Tailwind generators locate a spelling in Node, through a copy of the corpora's names and variables in
// tools/corpus.mjs. Every line of that table must pair a name with its variable exactly as All does, and
// name every corpus, so a corpus added or renamed here cannot leave the generators reading another root.
func TestTheGeneratorsNameTheSameCorpora(test *testing.T) {
	test.Parallel()
	path := filepath.Join("..", "lint", "rules", "tailwind", "tools", "corpus.mjs")
	source, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	_, table, found := strings.Cut(string(source), "const corpusVariables = {\n")
	table, _, closed := strings.Cut(table, "\n};")
	if !found || !closed {
		test.Fatalf("%s has no `const corpusVariables = { ... };` table", path)
	}
	entry := regexp.MustCompile(`^    '([a-z][a-z0-9-]+)': '([A-Z_]+)',$`)
	got := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		match := entry.FindStringSubmatch(line)
		if match == nil {
			test.Fatalf("%s's table holds %q, which is not a `'name': 'VARIABLE',` line", path, line)
		}
		got[match[1]] = match[2]
	}
	want := map[string]string{}
	for _, corpus := range All {
		want[corpus.Name] = corpus.Variable
	}
	if !reflect.DeepEqual(got, want) {
		test.Errorf("%s names %v; internal/corpus names %v", path, got, want)
	}
}

// Locate reads the variable first and the corpora file second, says which to set when neither names the
// corpus, refuses a corpus that does not exist, and passes a path through as it was given.
func TestLocateReadsTheVariableThenTheCorporaFile(test *testing.T) {
	test.Parallel()
	configPath := writeConfig(test, `{"ahra": "/checkouts/ahra"}`)
	fromConfig := func() (string, error) { return configPath, nil }
	for name, testCase := range map[string]struct {
		spelling  string
		variables map[string]string
		want      string
	}{
		"from the variable":     {spelling: "ahra:app/theme.css", variables: map[string]string{Ahra.Variable: "/elsewhere/ahra"}, want: "/elsewhere/ahra/app/theme.css"},
		"from the corpora file": {spelling: "ahra:app/theme.css", want: "/checkouts/ahra/app/theme.css"},
		"the root":              {spelling: "ahra:", want: "/checkouts/ahra"},
		"a path":                {spelling: "/checkouts/other/theme.css", want: "/checkouts/other/theme.css"},
	} {
		got, err := locate(testCase.spelling, lookupFrom(testCase.variables), fromConfig)
		if err != nil || got != filepath.FromSlash(testCase.want) {
			test.Errorf("%s: located %q, %v; want %q", name, got, err, testCase.want)
		}
	}
	if _, err := locate("connected:app/theme.css", lookupFrom(nil), fromConfig); err == nil || !strings.Contains(err.Error(), Connected.Variable) {
		test.Errorf("a corpus named nowhere located with %v; want an error naming %s", err, Connected.Variable)
	}
	if _, err := locate("arha:app/theme.css", lookupFrom(nil), fromConfig); err == nil {
		test.Error("a misspelled corpus was located")
	}
}
