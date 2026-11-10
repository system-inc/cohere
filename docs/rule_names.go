// Package docs holds the website's sources, and reads the one piece of its generated data a program needs: every
// engine's rule names, from data/rules.json, which internal/docs_data/tools/generate writes from the registries. It
// sits above data/ because every file under data/ is one the generator builds.
//
// The Go engine's file-name rule reads them (#2xt6v5r). cohere's Go source names a file for the rule it ports, and the
// rule is a TypeScript one, `max-lines` in internal/lint/rules/core/max_lines.go, which the Go engine's own registry
// does not hold and whose packages it must not link. data/rules.json is that registry's names, with the Go engine's
// and the Swift engine's beside them: the generated catalog of every registry, so the rule decides from data rather
// than from a list of its own. internal/docs_data's freshness tests hold the file to what the generator renders now,
// and TestTheDocsPackageReadsTheRenderedRuleNames holds these names to it, so the copy a binary carries is never
// stale.
package docs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

// rulesFile is data/rules.json, byte for byte.
//
//go:embed data/rules.json
var rulesFile []byte

// RuleNames are the names of every rule data/rules.json lists, in every engine, in its order, read once. A file that
// will not decode panics, which is the loud failure: the generator writes it and its tests read it, so a refusal here
// is a broken build rather than a state a run can recover from.
var RuleNames = sync.OnceValue(func() []string {
	var file struct {
		Rules []struct {
			Name string `json:"name"`
		} `json:"rules"`
	}
	if decodeError := json.Unmarshal(rulesFile, &file); decodeError != nil {
		// Invariant: data/rules.json is embedded and generated, so it decodes in every build its tests pass in.
		panic(fmt.Sprintf("docs/data/rules.json: %v", decodeError))
	}
	names := make([]string, 0, len(file.Rules))
	for _, entry := range file.Rules {
		names = append(names, entry.Name)
	}
	return names
})
