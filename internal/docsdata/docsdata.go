// Package docsdata generates the data the cohere website (system.inc/cohere) is built from, from the
// sources the binary itself is built from, so the site can never say something the binary does not do.
//
// Nothing here is typed in. Each output reads one source the build already trusts: the rule registry
// and what each registration declares, the rule sets cohere carries as the loader resolves them, the
// Swift registry and verdict catalog, the Go engine's registry, the settings schemas
// internal/settingsschema renders, CHANGELOG.md, the flag set as the binary prints it, the cases the rule
// tests assert, recorded by the capture hook in internal/lint/testing, and the benchmark runs
// bench/quiet_machine.sh recorded in bench/results. A field no source holds yet is left out rather than
// guessed, and the sourceNotes in rules.json say where each field comes from.
//
// The per-rule .md files beside the rules are internal working notes and are never read here.
//
// Run `go run ./internal/docsdata/tools/generate` from the module root after a change. `-check` recaptures
// the examples from the lint tests and exits 1 on any stale file, writing nothing; `-check-rendering` is
// the fast check of everything rendered from the committed examples. The website vendors docs/data/ at a
// pinned commit.
package docsdata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/system-inc/cohere/internal/benchresults"
	goenginerule "github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// The files Build produces, relative to the module root.
const (
	Directory          = "docs/data"
	RulesPath          = Directory + "/rules.json"
	SetsPath           = Directory + "/sets.json"
	CliPath            = Directory + "/cli.json"
	ChangelogPath      = Directory + "/changelog.json"
	BenchmarksPath     = Directory + "/benchmarks.json"
	ExamplesDirectory  = Directory + "/examples"
	settingsSchemaName = "CohereSettings.schema.json"
)

// Inputs is every source Build reads, gathered by the generator, so a test can hand it a changed
// registry or a changed set and watch the output move.
type Inputs struct {
	// Registrations is every registered rule, as rule.Registered returns them.
	Registrations []rule.Entry

	// SetNames is every rule set cohere carries, prefixed (`cohere:typescript`), and LoadSet resolves
	// one the way a project extending it alone would see it. SetContents is a set's own text.
	SetNames    []string
	LoadSet     func(name string) (*configuration.Config, error)
	SetContents func(name string) ([]byte, error)

	// SwiftVerdicts is swift/HouseRuleVerdicts.json.
	SwiftVerdicts []byte

	// SwiftRules is swift/Rules.json, the Swift engine's registry as data, which a Swift test holds to
	// the registry in both directions.
	SwiftRules []byte

	// GoRules is the Go engine's registry, read in process rather than from a data file, since that
	// engine is Go and compiled into the same binary as this package.
	GoRules []goenginerule.Rule

	// SettingsSchemas is what internal/settingsschema renders, keyed by its path relative to the module
	// root. The *.schema.json files are copied beside the rest, so the site reads one directory.
	SettingsSchemas map[string][]byte

	// Changelog is CHANGELOG.md.
	Changelog []byte

	// Benchmarks is every recorded benchmark run in bench/results, as internal/benchresults reads them.
	Benchmarks []benchresults.Record

	// Help is what `cohere -help` writes, and VerbHelp what each verb's `-help` writes, keyed by verb.
	// Nil leaves cli.json out of Build's output, for a caller that did not build the binary.
	Help     []byte
	VerbHelp map[string][]byte

	// Examples is each rule's chosen cases, keyed by rule name: freshly picked from a capture, or the
	// committed files read back.
	Examples map[string]Examples
}

// Build renders every generated file, keyed by its path relative to the module root.
func Build(inputs Inputs) (map[string][]byte, error) {
	files := map[string][]byte{}

	registered := map[string]bool{}
	for _, registration := range inputs.Registrations {
		registered[registration.Name] = true
	}
	for name, examples := range inputs.Examples {
		if !registered[name] {
			// A rule renamed or removed leaves its examples behind; they are dropped rather than
			// published under a name the binary no longer answers to.
			continue
		}
		encoded, encodeError := encode(examples)
		if encodeError != nil {
			return nil, encodeError
		}
		files[ExamplePath(name)] = encoded
	}

	sets, buildSetsError := buildSets(inputs)
	if buildSetsError != nil {
		return nil, buildSetsError
	}
	encodedSets, encodeError := encode(sets)
	if encodeError != nil {
		return nil, encodeError
	}
	files[SetsPath] = encodedSets

	rules, buildRulesError := buildRules(inputs, sets)
	if buildRulesError != nil {
		return nil, buildRulesError
	}
	encodedRules, encodeError := encode(rules)
	if encodeError != nil {
		return nil, encodeError
	}
	files[RulesPath] = encodedRules

	changelog, parseChangelogError := parseChangelog(inputs.Changelog)
	if parseChangelogError != nil {
		return nil, parseChangelogError
	}
	encodedChangelog, encodeError := encode(changelog)
	if encodeError != nil {
		return nil, encodeError
	}
	files[ChangelogPath] = encodedChangelog

	benchmarks, buildBenchmarksError := buildBenchmarks(inputs.Benchmarks)
	if buildBenchmarksError != nil {
		return nil, buildBenchmarksError
	}
	encodedBenchmarks, encodeError := encode(benchmarks)
	if encodeError != nil {
		return nil, encodeError
	}
	files[BenchmarksPath] = encodedBenchmarks

	if inputs.Help != nil {
		cli, parseCommandLineInterfaceError := parseCli(inputs.Help, inputs.VerbHelp)
		if parseCommandLineInterfaceError != nil {
			return nil, parseCommandLineInterfaceError
		}
		encodedCommandLineInterface, encodeError := encode(cli)
		if encodeError != nil {
			return nil, encodeError
		}
		files[CliPath] = encodedCommandLineInterface
	}

	for path, contents := range inputs.SettingsSchemas {
		if filepath.Ext(path) != ".json" {
			continue
		}
		files[Directory+"/"+filepath.Base(path)] = contents
	}
	if _, present := files[Directory+"/"+settingsSchemaName]; !present {
		return nil, fmt.Errorf("docsdata: internal/settingsschema rendered no %s", settingsSchemaName)
	}
	return files, nil
}

// ExamplePath is where one rule's examples are written. A namespaced rule's slashes become
// directories, as its name already reads like a path: examples/@typescript-eslint/no-explicit-any.json.
func ExamplePath(ruleName string) string {
	return ExamplesDirectory + "/" + ruleName + ".json"
}

// Stale returns the paths under root whose contents differ from files, sorted, plus every example file
// on disk Build no longer produces: a rule renamed leaves one behind, and -check names it.
func Stale(root string, files map[string][]byte) ([]string, error) {
	var stale []string
	for path, contents := range files {
		// Ignored: a file missing or unreadable is not what Build produced, so its nil contents read as stale,
		// which is the answer either way.
		existing, _ := os.ReadFile(filepath.Join(root, path))
		if !bytes.Equal(existing, contents) {
			stale = append(stale, path)
		}
	}
	orphans, orphanedExamplesError := OrphanedExamples(root, files)
	if orphanedExamplesError != nil {
		return nil, orphanedExamplesError
	}
	stale = append(stale, orphans...)
	sort.Strings(stale)
	return stale, nil
}

// OrphanedExamples returns the example files under root that files does not hold, sorted.
func OrphanedExamples(root string, files map[string][]byte) ([]string, error) {
	var orphans []string
	directory := filepath.Join(root, ExamplesDirectory)
	walkError := filepath.WalkDir(directory, func(path string, entry os.DirEntry, pathError error) error {
		if os.IsNotExist(pathError) && path == directory {
			return filepath.SkipDir
		}
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		relative, relativePathError := filepath.Rel(root, path)
		if relativePathError != nil {
			return relativePathError
		}
		relative = filepath.ToSlash(relative)
		if _, produced := files[relative]; !produced {
			orphans = append(orphans, relative)
		}
		return nil
	})
	sort.Strings(orphans)
	return orphans, walkError
}

// encode writes a value with four-space indentation and no HTML escaping, so a regeneration diffs
// cleanly. Every value Build encodes is a struct or a slice in a fixed order, and a map only where
// encoding/json sorts its keys, so two runs give the same bytes. Value is whichever of those types a file
// holds.
func encode[Value any](value Value) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	if encodeError := encoder.Encode(value); encodeError != nil {
		return nil, encodeError
	}
	return buffer.Bytes(), nil
}
