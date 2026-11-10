package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
)

// Every set cohere carries loads against the real registry, and every rule key each one writes, at top level
// or in an override, reaches a registered rule (#yv90vjs). The load refuses a key that reaches none, so a set
// failing here would fail every project that extends it, on every run.
//
// The keys are judged here as well as by the load, through KeyReachesRule, the resolver's own test, so the
// guard does not rest on the refusal it guards: a load that let an unknown key through would still fail here.
//
// Here rather than beside the sets, because it needs the whole rule set, and the configuration package's test
// binary does not link it (see BenchmarkLoadForTheLiveConfiguration).
func TestEveryKeyInEverySetReachesARegisteredRule(test *testing.T) {
	test.Parallel()
	names := Names()
	if len(names) == 0 {
		test.Fatal("the registry is empty, so this test proves nothing")
	}
	setNames := configuration.SetNames()
	if len(setNames) < 3 {
		test.Fatalf("cohere carries %d sets (%v); the embedded tree did not load", len(setNames), setNames)
	}
	for _, setName := range setNames {
		directory := test.TempDir()
		path := filepath.Join(directory, "CohereSettings.json")
		if writeError := os.WriteFile(path, []byte(`{"extends": "`+setName+`"}`), 0o600); writeError != nil {
			test.Fatal(writeError)
		}
		if _, loadForError := configuration.LoadFor(path, names); loadForError != nil {
			test.Errorf("%s does not load against the registry: %v", setName, loadForError)
		}

		contents, sourceContentsError := configuration.SourceContents(setName)
		if sourceContentsError != nil {
			test.Fatal(sourceContentsError)
		}
		var set struct {
			Rules     map[string]json.RawMessage `json:"rules"`
			Overrides []struct {
				Rules map[string]json.RawMessage `json:"rules"`
			} `json:"overrides"`
		}
		if decodeError := json.Unmarshal(contents, &set); decodeError != nil {
			test.Fatalf("parsing %s: %v", setName, decodeError)
		}
		keys := make([]string, 0, len(set.Rules))
		for key := range set.Rules {
			keys = append(keys, key)
		}
		for _, override := range set.Overrides {
			for key := range override.Rules {
				keys = append(keys, key)
			}
		}
		for _, key := range keys {
			if key == configuration.UnusedDisable || reachesARegisteredRule(key, names) {
				continue
			}
			test.Errorf("%s names %q, which reaches no registered rule", setName, key)
		}
	}
}

func reachesARegisteredRule(key string, names []string) bool {
	for _, name := range names {
		if configuration.KeyReachesRule(key, name) {
			return true
		}
	}
	return false
}
