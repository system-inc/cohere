package corpus

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func lookupFrom(variables map[string]string) func(string) string {
	return func(name string) string { return variables[name] }
}

// Every corpus has a name, a variable and a description, and no two share a name or a variable, so the
// config and the gate's line cannot confuse them.
func TestEveryCorpusIsNamedOnce(t *testing.T) {
	t.Parallel()
	names, variables := map[string]bool{}, map[string]bool{}
	for _, corpus := range All {
		if corpus.Name == "" || corpus.Variable == "" || corpus.What == "" {
			t.Errorf("%+v is missing a name, variable or description", corpus)
		}
		if names[corpus.Name] || variables[corpus.Variable] {
			t.Errorf("%s or %s is used twice", corpus.Name, corpus.Variable)
		}
		names[corpus.Name], variables[corpus.Variable] = true, true
	}
}

func TestUncoveredIsEveryUnsetCorpus(t *testing.T) {
	t.Parallel()
	got := Uncovered(lookupFrom(map[string]string{Ahra.Variable: "/somewhere"}))
	want := []Corpus{Structure, Connected, PrettierFork}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uncovered %v, want %v", got, want)
	}
	if got := Uncovered(lookupFrom(nil)); len(got) != len(All) {
		t.Errorf("with nothing set, %d uncovered, want all %d", len(got), len(All))
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corpora.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A config fills only what the environment leaves unset, so a run can still point one corpus elsewhere.
func TestEnvironmentFillsOnlyUnsetVariables(t *testing.T) {
	t.Parallel()
	config, err := ReadConfig(writeConfig(t, `{"ahra": "/checkouts/ahra", "structure": "/checkouts/structure"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := Environment(config, lookupFrom(map[string]string{Structure.Variable: "/elsewhere"}))
	if want := []string{"COHERE_CORPUS_AHRA=/checkouts/ahra"}; !reflect.DeepEqual(got, want) {
		t.Errorf("environment %v, want %v", got, want)
	}
}

// A missing file is no corpora; a typo or a relative path is refused rather than leaving a corpus
// quietly uncovered.
func TestReadConfigRefusesWhatWouldLoseACorpus(t *testing.T) {
	t.Parallel()
	if config, err := ReadConfig(filepath.Join(t.TempDir(), "absent.json")); err != nil || len(config) != 0 {
		t.Errorf("a missing file read as %v, %v", config, err)
	}
	for name, contents := range map[string]string{
		"a name no corpus has": `{"arha": "/checkouts/ahra"}`,
		"a relative path":      `{"ahra": "checkouts/ahra"}`,
		"not an object":        `["/checkouts/ahra"]`,
	} {
		if _, err := ReadConfig(writeConfig(t, contents)); err == nil {
			t.Errorf("%s was read", name)
		}
	}
}

// Root skips naming the variable when it is unset, and fails when it is set to something that is not a
// directory. Each is run in a child process, since a skip and a failure end the test that meets them.
func TestRootSkipsWhenUnsetAndFailsWhenWrong(t *testing.T) {
	t.Parallel()
	if probe := os.Getenv("COHERE_CORPUS_PROBE"); probe != "" {
		Ahra.Root(t)
		return
	}
	for name, testCase := range map[string]struct {
		value string
		want  string
	}{
		"unset":           {value: "", want: "--- SKIP"},
		"not a directory": {value: filepath.Join(t.TempDir(), "absent"), want: "--- FAIL"},
		"a directory":     {value: t.TempDir(), want: "--- PASS"},
	} {
		output := runProbe(t, testCase.value)
		if !strings.Contains(output, testCase.want) {
			t.Errorf("%s: want %s in:\n%s", name, testCase.want, output)
		}
		if testCase.want == "--- SKIP" && !strings.Contains(output, Ahra.Variable) {
			t.Errorf("the skip does not name %s:\n%s", Ahra.Variable, output)
		}
	}
}
