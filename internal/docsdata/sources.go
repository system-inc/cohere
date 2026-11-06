package docsdata

import (
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/benchresults"
	"github.com/system-inc/cohere/internal/goengine/rules"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/settingsschema"
)

// SourceInputs gathers every input but the binary's help from the module at root: the live registry
// and sets, the files beside them, and the committed examples. The generator adds the help, and
// replaces the examples when it recaptures.
func SourceInputs(root string) (Inputs, error) {
	names := registry.Names()
	inputs := Inputs{
		Registrations: rule.Registered(),
		SetNames:      configuration.SetNames(),
		LoadSet: func(
			name string,
		) (*configuration.Configuration, error) {
			return configuration.LoadFor(name, names)
		},
		SetContents: configuration.SourceContents,
		GoRules:     rules.All(),
	}
	var readError error
	if inputs.SwiftVerdicts, readError = os.ReadFile(
		filepath.Join(root, "swift", "HouseRuleVerdicts.json"),
	); readError != nil {
		return inputs, readError
	}
	if inputs.SwiftRules, readError = os.ReadFile(filepath.Join(root, "swift", "Rules.json")); readError != nil {
		return inputs, readError
	}
	if inputs.Changelog, readError = os.ReadFile(filepath.Join(root, "CHANGELOG.md")); readError != nil {
		return inputs, readError
	}
	var buildError error
	if inputs.SettingsSchemas, buildError = settingsschema.Build(names); buildError != nil {
		return inputs, buildError
	}
	if inputs.Benchmarks, readError = benchresults.Read(filepath.Join(root, benchresults.Directory)); readError != nil {
		return inputs, readError
	}
	var readCommittedExamplesError error
	inputs.Examples, readCommittedExamplesError = ReadCommittedExamples(root)
	return inputs, readCommittedExamplesError
}
