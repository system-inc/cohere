package corpus

import (
	"os"
	"os/exec"
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

func writeConfiguration(test *testing.T, contents string) string {
	test.Helper()
	path := filepath.Join(test.TempDir(), "corpora.json")
	if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

// A config fills only what the environment leaves unset, so a run can still point one corpus elsewhere.
func TestEnvironmentFillsOnlyUnsetVariables(test *testing.T) {
	test.Parallel()
	configuration, readConfigurationError := ReadConfiguration(
		writeConfiguration(test, `{"ahra": "/checkouts/ahra", "structure": "/checkouts/structure"}`),
	)
	if readConfigurationError != nil {
		test.Fatal(readConfigurationError)
	}
	got := Environment(configuration, lookupFrom(map[string]string{Structure.Variable: "/elsewhere"}))
	if want := []string{"COHERE_CORPUS_AHRA=/checkouts/ahra"}; !reflect.DeepEqual(got, want) {
		test.Errorf("environment %v, want %v", got, want)
	}
}

// A missing file is no corpora; a typo or a relative path is refused rather than leaving a corpus
// quietly uncovered.
func TestReadConfigRefusesWhatWouldLoseACorpus(test *testing.T) {
	test.Parallel()
	if configuration, readConfigurationError := ReadConfiguration(
		filepath.Join(test.TempDir(), "absent.json"),
	); readConfigurationError != nil || len(configuration) != 0 {
		test.Errorf("a missing file read as %v, %v", configuration, readConfigurationError)
	}
	for name, contents := range map[string]string{
		"a name no corpus has": `{"arha": "/checkouts/ahra"}`,
		"a relative path":      `{"ahra": "checkouts/ahra"}`,
		"not an object":        `["/checkouts/ahra"]`,
	} {
		if _, readConfigurationError := ReadConfiguration(
			writeConfiguration(test, contents),
		); readConfigurationError == nil {
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
		corpus, inside, spelled, spelledError := Spelled(spelling)
		if spelledError != nil || corpus != want.corpus || inside != want.inside || spelled != want.spelled {
			test.Errorf(
				"Spelled(%q) = %v, %q, %v, %v; want %v, %q, %v",
				spelling,
				corpus,
				inside,
				spelled,
				spelledError,
				want.corpus,
				want.inside,
				want.spelled,
			)
		}
	}
	if _, _, spelled, spelledError := Spelled(
		"arha:app/_theme/styles/theme.css",
	); !spelled || spelledError == nil || !strings.Contains(spelledError.Error(), "no corpus") {
		test.Errorf("a misspelled corpus read as %v, %v; want it refused", spelled, spelledError)
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
	if createDirectoryError := os.MkdirAll(filepath.Join(root, "app", "styles"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(filepath.Join(root, "app", "styles", "theme.css"), nil, 0o644); writeError != nil {
		test.Fatal(writeError)
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
		"an absolute path": {
			spelling: filepath.Join(root, "app", "styles", "theme.css"),
			value:    root,
			want:     "--- FAIL",
		},
		"not in the repository": {spelling: "app/styles/theme.css", want: "--- FAIL"},
		"in the repository":     {spelling: "internal/corpus/corpus.go", want: "--- PASS"},
		"out of the repository": {spelling: "..", want: "--- FAIL"},
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
	source, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	_, table, found := strings.Cut(string(source), "const corpusVariables = {\n")
	table, _, closed := strings.Cut(table, "\n};")
	if !found || !closed {
		test.Fatalf("%s has no `const corpusVariables = { ... };` table", path)
	}
	// A name is quoted only where it must be, a hyphenated one, since the house format the file is held to takes
	// the quotes off the rest (#4wjhppw).
	entry := regexp.MustCompile(`^    (?:'([a-z][a-z0-9]*-[a-z0-9-]+)'|([a-z][a-z0-9]*)): '([A-Z_]+)',$`)
	got := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		match := entry.FindStringSubmatch(line)
		if match == nil {
			test.Fatalf("%s's table holds %q, which is not a `name: 'VARIABLE',` line", path, line)
		}
		got[match[1]+match[2]] = match[3]
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
	configurationPath := writeConfiguration(test, `{"ahra": "/checkouts/ahra"}`)
	fromConfiguration := func() (string, error) { return configurationPath, nil }
	for name, testCase := range map[string]struct {
		spelling  string
		variables map[string]string
		want      string
	}{
		"from the variable": {
			spelling:  "ahra:app/theme.css",
			variables: map[string]string{Ahra.Variable: "/elsewhere/ahra"},
			want:      "/elsewhere/ahra/app/theme.css",
		},
		"from the corpora file": {spelling: "ahra:app/theme.css", want: "/checkouts/ahra/app/theme.css"},
		"the root":              {spelling: "ahra:", want: "/checkouts/ahra"},
		"a path":                {spelling: "/checkouts/other/theme.css", want: "/checkouts/other/theme.css"},
	} {
		got, locateError := locate(testCase.spelling, lookupFrom(testCase.variables), fromConfiguration)
		if locateError != nil || got != filepath.FromSlash(testCase.want) {
			test.Errorf("%s: located %q, %v; want %q", name, got, locateError, testCase.want)
		}
	}
	if _, locateError := locate(
		"connected:app/theme.css",
		lookupFrom(nil),
		fromConfiguration,
	); locateError == nil || !strings.Contains(locateError.Error(), Connected.Variable) {
		test.Errorf("a corpus named nowhere located with %v; want an error naming %s", locateError, Connected.Variable)
	}
	if _, locateError := locate("arha:app/theme.css", lookupFrom(nil), fromConfiguration); locateError == nil {
		test.Error("a misspelled corpus was located")
	}
}

// At reads a corpus as it stood at a commit, whatever its checkout holds now, and the live checkout only when
// LiveVariable is set; a checkout without the commit fails, never reads something else (#jf5dz34). Shown through a
// child of this binary over a repository whose file reads "pinned" at one commit and "live" in its working tree.
func TestAtReadsTheCommitNotTheCheckout(test *testing.T) {
	test.Parallel()
	if probe := os.Getenv("COHERE_CORPUS_PROBE"); strings.HasPrefix(probe, "at ") {
		mode, commit, _ := strings.Cut(strings.TrimPrefix(probe, "at "), " ")
		if mode == "live" {
			// The child is its own process, so its environment is its own to change.
			if setError := os.Setenv(LiveVariable, "1"); setError != nil {
				test.Fatal(setError)
			}
		}
		contents, readError := os.ReadFile(Ahra.At(test, commit, "source", "file.ts"))
		if readError != nil {
			test.Fatal(readError)
		}
		test.Logf("read %q", strings.TrimSpace(string(contents)))
		return
	}
	repository := test.TempDir()
	git := func(arguments ...string) string {
		test.Helper()
		command := exec.Command("git", append([]string{"-C", repository, "-c", "user.name=test", "-c",
			"user.email=test@example.com"}, arguments...)...)
		output, gitError := command.CombinedOutput()
		if gitError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q")
	writeFile := func(contents string) {
		test.Helper()
		if writeError := os.MkdirAll(filepath.Join(repository, "source"), 0o755); writeError != nil {
			test.Fatal(writeError)
		}
		if writeError := os.WriteFile(filepath.Join(repository, "source", "file.ts"), []byte(contents),
			0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	writeFile("pinned\n")
	git("add", ".")
	git("commit", "-q", "-m", "pinned")
	pinned := git("rev-parse", "HEAD")
	writeFile("live\n")

	for name, testCase := range map[string]struct {
		probe string
		want  []string
	}{
		"the pinned commit": {probe: "at pinned " + pinned, want: []string{"--- PASS", `read "pinned"`}},
		"the live checkout": {probe: "at live " + pinned, want: []string{"--- PASS", `read "live"`}},
		"a commit it lacks": {
			probe: "at pinned " + strings.Repeat("1", 40),
			want:  []string{"--- FAIL", "holds no commit"},
		},
	} {
		output := runProbe(test, "TestAtReadsTheCommitNotTheCheckout", testCase.probe, repository)
		for _, want := range testCase.want {
			if !strings.Contains(output, want) {
				test.Errorf("%s: want %q in:\n%s", name, want, output)
			}
		}
	}
}
