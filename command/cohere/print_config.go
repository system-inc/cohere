package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

var printConfigAliases = map[string]string{
	"nexus/require-await":                "require-await",
	"nexus/no-use-before-define":         "no-use-before-define",
	"nexus/logical-assignment-operators": "logical-assignment-operators",
	"nexus/no-implicit-coercion":         "no-implicit-coercion",
	"nexus/max-classes-per-file":         "max-classes-per-file",
}

type printedOverride struct {
	Index  int                          `json:"index"`
	File   string                       `json:"file"`
	Files  []string                     `json:"files"`
	Reason string                       `json:"reason"`
	Rules  map[string][]json.RawMessage `json:"rules"`
}

type printedDeparture struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// writeResolvedConfig answers language questions from the same program the lint run builds.
// Globals come from TypeScript declarations, including the selected standard libraries; naming the
// declaration files explains their origin without pretending ESLint's writable/readonly knob exists.
func writeResolvedConfig(out io.Writer, config *configuration.Config, resolved configuration.Resolved, location projectLocation, path string) error {
	base := optionsBase(config, location.Root)
	if err := registry.CheckSettings(base); err != nil {
		return fmt.Errorf("validating the lint config: %w", err)
	}
	settings, err := settingsForFile(base, resolved, rule.RegisteredSettings())
	if err != nil {
		return err
	}
	graph, err := program.Build(program.Options{ConfigFileName: location.ConfigFileName, CurrentDirectory: location.Root, SingleThreaded: true})
	if err != nil {
		return err
	}
	if !tspath.IsRootedDiskPath(path) {
		path = absoluteFrom(config.Root, path)
	}
	source := graph.Program.GetSourceFile(tspath.RootedFilePath(path))
	if source == nil {
		return fmt.Errorf("%s does not belong to %s", path, graph.ConfigFileName)
	}
	sourceType := "script"
	if ast.IsExternalModule(source) {
		sourceType = "module"
	} else if source.CommonJSModuleIndicator != nil {
		sourceType = "commonjs"
	}
	globals := map[string][]string{}
	checker, release := graph.CheckerForFile(context.Background(), source)
	defer release()
	for _, symbol := range checker.GetSymbolsInScope(source.AsNode(), ast.SymbolFlagsValue) {
		global := checker.GetGlobalSymbol(symbol.Name, ast.SymbolFlagsValue, nil)
		if global == nil {
			continue
		}
		declarations := []string{}
		for _, declaration := range global.Declarations {
			if file := ast.GetSourceFileOfNode(declaration); file != nil {
				declarations = append(declarations, string(file.FileName()))
			}
		}
		slices.Sort(declarations)
		globals[symbol.Name] = slices.Compact(declarations)
	}
	overrides := []printedOverride{}
	for _, index := range resolved.MatchedOverrideIndices() {
		override := config.Overrides[index]
		rules := map[string][]json.RawMessage{}
		for name, setting := range override.Rules {
			rules[name] = append([]json.RawMessage{json.RawMessage(strconv.Itoa(int(setting.Severity)))}, setting.Options...)
		}
		overrides = append(overrides, printedOverride{Index: index, File: override.File, Files: override.Files, Reason: override.Reason, Rules: rules})
	}
	departures := map[string]printedDeparture{}
	for name, departure := range config.Departures {
		departures[name] = printedDeparture{File: departure.File, Reason: departure.Reason}
	}
	// Struct field order fixes the public top-level order; encoding/json sorts every map's keys.
	document := struct {
		Settings        map[string]json.RawMessage `json:"settings"`
		LanguageOptions struct {
			SourceType string              `json:"sourceType"`
			Target     string              `json:"target"`
			Tsconfig   string              `json:"tsconfig"`
			Globals    map[string][]string `json:"globals"`
		} `json:"languageOptions"`
		Overrides   []printedOverride            `json:"overrides"`
		Departures  map[string]printedDeparture  `json:"departures"`
		RuleAliases map[string]string            `json:"ruleAliases"`
		Rules       map[string][]json.RawMessage `json:"rules"`
	}{Settings: settings, Overrides: overrides, Departures: departures, RuleAliases: printConfigAliases, Rules: resolvedConfigRules(resolved)}
	document.LanguageOptions.SourceType = sourceType
	document.LanguageOptions.Target = strings.ToLower(graph.Program.Options().GetEmitScriptTarget().String())
	document.LanguageOptions.Tsconfig = graph.ConfigFileName
	document.LanguageOptions.Globals = globals
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(document)
}

// settingsForFile asks the registered readers which keys enabled rules consume. Settings keep their
// configured values (paths are relative to ConfigDirectory), rather than mixing in a rule's own
// options or inventing plugin defaults. A later settings reader needs no change to this command.
func settingsForFile(base rule.OptionsBase, resolved configuration.Resolved, registrations []rule.SettingsRegistration) (map[string]json.RawMessage, error) {
	settings := map[string]json.RawMessage{}
	for _, registration := range registrations {
		for _, namespace := range registration.Namespaces {
			raw, written := base.Settings[namespace]
			if !written {
				continue
			}
			parts, err := registration.Split(raw)
			if err != nil {
				return nil, fmt.Errorf("settings[%q]: %w", namespace, err)
			}
			read := map[string]json.RawMessage{}
			for name, part := range parts {
				if !resolved.Enabled(name) {
					continue
				}
				var keys map[string]json.RawMessage
				if err := json.Unmarshal(part, &keys); err != nil {
					return nil, fmt.Errorf("settings[%q], as %s reads it: %w", namespace, name, err)
				}
				for key, value := range keys {
					read[key] = value
				}
			}
			if len(read) > 0 {
				encoded, err := json.Marshal(read)
				if err != nil {
					return nil, err
				}
				settings[namespace] = encoded
			}
		}
	}
	return settings, nil
}

// resolvedConfigRules preserves the registered-rule view and adds the five house spellings whose
// settings already reach core implementations. A configured unused-disable, when registered, follows
// the same loop as every other rule; a build without it invents no dead-directive setting.
func resolvedConfigRules(resolved configuration.Resolved) map[string][]json.RawMessage {
	rules := map[string][]json.RawMessage{}
	for _, registered := range registry.All() {
		status, setting := resolved.StatusOf(registered.Name)
		var severity int
		switch status {
		case configuration.StatusEnabled:
			severity = int(setting.Severity)
		case configuration.StatusScopedOff:
			severity = 0
		default:
			continue
		}
		entry := []json.RawMessage{json.RawMessage(strconv.Itoa(severity))}
		entry = append(entry, setting.Options...)
		rules[registered.Name] = entry
	}
	for alias, name := range printConfigAliases {
		if entry, found := rules[name]; found {
			rules[alias] = entry
		}
	}
	return rules
}
