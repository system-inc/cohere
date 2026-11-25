package documentation_data

import (
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/bench_results"
	"github.com/system-inc/cohere/internal/go_language"
	"github.com/system-inc/cohere/internal/go_language/rules"
	"github.com/system-inc/cohere/internal/settings"
	"github.com/system-inc/cohere/internal/settings/schema"
	"github.com/system-inc/cohere/internal/typescript_language/registry"
	"github.com/system-inc/cohere/internal/typescript_language/rule"
)

// SourceInputs gathers every input but the binary's help from the module at root: the live registry
// and sets, the files beside them, and the committed examples. The generator adds the help, and
// replaces the examples when it recaptures.
func SourceInputs(root string) (Inputs, error) {
	names := registry.Names()
	inputs := Inputs{
		Registrations: rule.Registered(),
		SetNames:      settings.SetNames(),
		LoadSet: func(
			name string,
		) (*settings.Configuration, error) {
			return settings.LoadFor(name, names)
		},
		SetContents:   settings.SourceContents,
		GoRules:       rules.All(),
		GoFixingRules: go_language.FixingRules(),
	}
	var readError error
	if inputs.SwiftVerdicts, readError = os.ReadFile(
		filepath.Join(root, "swift_language", "HouseRuleVerdicts.json"),
	); readError != nil {
		return inputs, readError
	}
	if inputs.SwiftRules, readError = os.ReadFile(
		filepath.Join(root, "swift_language", "Rules.json"),
	); readError != nil {
		return inputs, readError
	}
	if inputs.Changelog, readError = os.ReadFile(filepath.Join(root, "CHANGELOG.md")); readError != nil {
		return inputs, readError
	}
	var buildError error
	if inputs.SettingsSchemas, buildError = schema.Build(names); buildError != nil {
		return inputs, buildError
	}
	if inputs.Benchmarks, readError = bench_results.Read(
		filepath.Join(root, bench_results.Directory),
	); readError != nil {
		return inputs, readError
	}
	var readCommittedExamplesError error
	inputs.Examples, readCommittedExamplesError = ReadCommittedExamples(root)
	return inputs, readCommittedExamplesError
}
