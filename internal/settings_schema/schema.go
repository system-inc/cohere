// Package settings_schema generates the JSON schemas for CohereSettings.json and NexusCohereSettings.json,
// and the reference document beside them, from the types that read those files.
//
// Nothing here lists a key by hand. The keys come from the lint loader (configuration.TopLevelKeys and
// OverrideFields, read off the structs it decodes into), the format block's from format_options.BlockKeys
// (the table its decoder runs on), and the rule names from the registry. What is written by hand is the
// prose a person reads about each key, and the test refuses a key with no prose as firmly as prose for
// a key that no longer exists. Run `go run ./internal/settings_schema/tools/generate` from the module
// root after a change; the test fails until the committed files match.
package settings_schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/native"
	"github.com/system-inc/cohere/internal/go_engine/settings"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/our_tiers"
)

// The files Build produces, relative to the module root.
const (
	ProjectSchemaPath = "schema/CohereSettings.schema.json"
	NexusSchemaPath   = "schema/NexusCohereSettings.schema.json"
	ReferencePath     = "schema/CohereSettings.md"
)

// FormatKey is the formatter's block: optional in a project's settings, which it decides outside our tiers,
// and required in the Nexus tier.
const FormatKey = "format"

// SwiftKey is the Swift engine's block, which cohere-swift reads itself.
const SwiftKey = "swift"

// schemaNode is one node of a schema, in as much of JSON Schema 2020-12 as these schemas use. Its fields are
// declared in the byte order of their keys, the order encoding/json sorts a map's keys into, so a node encodes
// with its keys sorted, as the committed files are written. Minimum is a pointer, since a minimum of 0 is
// written and an unset one is not.
type schemaNode struct {
	Definitions          map[string]*schemaNode `json:"$defs,omitempty"`
	Reference            string                 `json:"$ref,omitempty"`
	Schema               string                 `json:"$schema,omitempty"`
	AdditionalProperties *additionalProperties  `json:"additionalProperties,omitempty"`
	Description          string                 `json:"description,omitempty"`
	Enum                 []string               `json:"enum,omitempty"`
	Examples             []string               `json:"examples,omitempty"`
	Items                *schemaNode            `json:"items,omitempty"`
	MaxItems             int                    `json:"maxItems,omitempty"`
	MinItems             int                    `json:"minItems,omitempty"`
	MinLength            int                    `json:"minLength,omitempty"`
	Minimum              *int                   `json:"minimum,omitempty"`
	OneOf                []*schemaNode          `json:"oneOf,omitempty"`
	Pattern              string                 `json:"pattern,omitempty"`
	PrefixItems          []*schemaNode          `json:"prefixItems,omitempty"`
	Properties           map[string]*schemaNode `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
	Title                string                 `json:"title,omitempty"`
	Type                 string                 `json:"type,omitempty"`
}

// Build renders every generated file, keyed by its path relative to the module root.
func Build(ruleNames []string) (map[string][]byte, error) {
	project, documentError := document(ruleNames, false)
	if documentError != nil {
		return nil, documentError
	}
	nexus, documentError := document(ruleNames, true)
	if documentError != nil {
		return nil, documentError
	}
	files := map[string][]byte{}
	for path, schema := range map[string]*schemaNode{ProjectSchemaPath: project, NexusSchemaPath: nexus} {
		encoded, encodeError := encode(schema)
		if encodeError != nil {
			return nil, encodeError
		}
		files[path] = encoded
	}
	files[ReferencePath] = []byte(reference(ruleNames))
	// In the house format, as the format check at cohere's root reads every file there (#4wjhppw).
	for path, contents := range files {
		formatted, formatHouseError := native.FormatHouse(path, contents)
		if formatHouseError != nil {
			return nil, formatHouseError
		}
		files[path] = formatted
	}
	return files, nil
}

// document is one schema: the project's, where `format` is optional, or the Nexus tier's, which requires it.
func document(ruleNames []string, nexusTier bool) (*schemaNode, error) {
	properties := map[string]*schemaNode{}
	for _, key := range configuration.TopLevelKeys() {
		description, described := topLevelDescriptions[key.Name]
		if !described {
			return nil, fmt.Errorf(
				"settings_schema: the loader accepts %q and nothing describes it; add it to topLevelDescriptions",
				key.Name,
			)
		}
		shape, keyShapeError := keyShape(key)
		if keyShapeError != nil {
			return nil, keyShapeError
		}
		shape.Description = description
		properties[key.Name] = shape
	}

	defs, definitionsError := definitions(ruleNames)
	if definitionsError != nil {
		return nil, definitionsError
	}

	title := "CohereSettings.json"
	summary := "cohere's settings for one repository: what it checks, how strictly, how it formats, and which paths " +
		"it never touches. With no file at all, cohere applies the house stack and the house format."
	if nexusTier {
		title = "NexusCohereSettings.json"
		summary = "The Nexus tier our repositories' settings reach. In a chain extending a cohere:system-inc set " +
			"it is the one file that holds the format block, so every one of our repositories formats the same way."
	}
	schema := &schemaNode{
		Schema:               "https://json-schema.org/draft/2020-12/schema",
		Title:                title,
		Description:          summary + " Generated by internal/settings_schema; see " + ReferencePath + ".",
		Type:                 "object",
		AdditionalProperties: noAdditionalProperties,
		Properties:           properties,
		Definitions:          defs,
	}
	if nexusTier {
		schema.Required = []string{FormatKey}
	}
	return schema, nil
}

// ignoredKeyShapes says what a key the loader accepts without decoding holds. The format block is built
// from format_options.BlockKeys.
var ignoredKeyShapes = map[string]func() *schemaNode{
	"$schema": func() *schemaNode { return &schemaNode{Type: "string"} },
	"jsPlugins": func() *schemaNode {
		return &schemaNode{Type: "array", Items: &schemaNode{Type: "string"}}
	},
	"output": func() *schemaNode {
		return &schemaNode{
			Type:                 "object",
			AdditionalProperties: noAdditionalProperties,
			Properties: map[string]*schemaNode{
				"phases": {
					Type:        "boolean",
					Description: "Put where the time went first in the footer's parentheses, as --phases does.",
				},
			},
		}
	},
	"go":      goShape,
	FormatKey: formatShape,
	SwiftKey:  swiftShape,
}

// keyShape is the schema for one top-level key's value, read off the Go type the loader decodes it into.
func keyShape(key configuration.TopLevelKey) (*schemaNode, error) {
	if key.Name == "settings" {
		return settingsShape(), nil
	}
	if key.Field == nil {
		shape, known := ignoredKeyShapes[key.Name]
		if !known {
			return nil, fmt.Errorf(
				"settings_schema: the loader accepts %q without decoding it, and ignoredKeyShapes does not say what it holds",
				key.Name,
			)
		}
		return shape(), nil
	}
	return typeShape(key.Name, key.Field)
}

// settingsShape is the namespaces a run reads settings for: better-tailwindcss, under either of the
// names upstream reads (tailwindSettingsNamespaces in internal/lint/rules/tailwind). Which keys each
// carries is checked by the run against the rules that read them (registry.CheckSettings), and a key
// no rule reads is refused there, so the schema names the namespaces and leaves their keys open.
func settingsShape() *schemaNode {
	namespace := &schemaNode{Type: "object"}
	return &schemaNode{
		Type:                 "object",
		AdditionalProperties: noAdditionalProperties,
		Properties: map[string]*schemaNode{
			"better-tailwindcss":               namespace,
			"eslint-plugin-better-tailwindcss": namespace,
		},
	}
}

var stringSliceType = reflect.TypeOf([]string{})

// typeShape maps a decoded Go type to its schema. A type it does not know is an error, so a field whose
// type changes (extends becoming a list, say) fails the generator rather than publishing a stale shape.
func typeShape(name string, fieldType reflect.Type) (*schemaNode, error) {
	switch {
	case fieldType == configuration.ExtendsListType:
		source := &schemaNode{
			Type:      "string",
			MinLength: 1,
			Examples:  configuration.SetNames(),
		}
		return &schemaNode{OneOf: []*schemaNode{
			source,
			{Type: "array", MinItems: 1, Items: source},
		}}, nil
	case fieldType == configuration.IgnoreEntryListType:
		return &schemaNode{Type: "array", Items: ignoreEntryShape()}, nil
	case fieldType.Kind() == reflect.String:
		return &schemaNode{Type: "string"}, nil
	case fieldType == stringSliceType:
		return &schemaNode{Type: "array", Items: &schemaNode{Type: "string"}}, nil
	case fieldType.Kind() == reflect.Map &&
		fieldType.Key().Kind() == reflect.String &&
		fieldType.Elem().Kind() == reflect.String:
		return &schemaNode{
			Type:                 "object",
			AdditionalProperties: &additionalProperties{schema: &schemaNode{Type: "string", MinLength: 1}},
		}, nil
	case fieldType.Kind() == reflect.Map &&
		fieldType.Key().Kind() == reflect.String &&
		fieldType.Elem() == configuration.RuleValueType:
		return &schemaNode{Reference: "#/$defs/rules"}, nil
	case fieldType.Kind() == reflect.Slice && fieldType.Elem().Kind() == reflect.Struct:
		return &schemaNode{Type: "array", Items: &schemaNode{Reference: "#/$defs/override"}}, nil
	}
	return nil, fmt.Errorf(
		"settings_schema: %q decodes into %s, which the schema has no shape for; teach typeShape",
		name,
		fieldType,
	)
}

// goReasonPattern matches a reason that is not blank: one character, at least, outside the White_Space set that
// strings.TrimSpace trims, which is what the Go engine's loader refuses a departure's reason by. A plain `\S` would
// differ from it on `U+0085` and `U+FEFF`, since JavaScript's whitespace is not the same set.
const goReasonPattern = `[^\t-\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]`

// ignoreEntryShape is one `ignorePatterns` entry, as configuration.IgnoreEntry reads it: a glob, or an object
// naming the glob and why, its kind and check from the loader's own lists, and no other key. The reason is held
// to goReasonPattern, since the loader trims it by the same set before refusing a blank one. Inside our tiers
// the loader also refuses an entry with no reason, which the schema cannot see from one file.
func ignoreEntryShape() *schemaNode {
	kinds := make([]string, 0, len(configuration.IgnoreKinds))
	for _, kind := range configuration.IgnoreKinds {
		kinds = append(kinds, string(kind))
	}
	checks := make([]string, 0, len(configuration.IgnoreChecks))
	for _, check := range configuration.IgnoreChecks {
		checks = append(checks, string(check))
	}
	return &schemaNode{OneOf: []*schemaNode{
		{Type: "string"},
		{
			Type:                 "object",
			AdditionalProperties: noAdditionalProperties,
			Required:             []string{"pattern"},
			Properties: map[string]*schemaNode{
				"pattern": {Type: "string", MinLength: 1, Description: ignoreEntryDescriptions["pattern"]},
				"reason":  {Type: "string", Pattern: goReasonPattern, Description: ignoreEntryDescriptions["reason"]},
				"kind":    {Type: "string", Enum: kinds, Description: ignoreEntryDescriptions["kind"]},
				"check":   {Type: "string", Enum: checks, Description: ignoreEntryDescriptions["check"]},
			},
		},
	}}
}

// goShape is the Go engine's block, as internal/go_engine/settings reads it: rules, each a severity or
// [severity] or [severity, options object], departures, each rule's reason for loosening it past the house,
// reasons, why each off is off, and targets, the platforms checked beside the ones the files name.
// Its severities are that loader's own list, exact, so the schema never allows what the engine refuses. Rule
// names are left open, in rules, departures and reasons alike, since the Go engine's registry is not the TypeScript
// one this package lists; the engine refuses a name it does not know. Which options loosen a rule is the rule's to
// say, so a departure excusing nothing is the engine's refusal, at configuration, and not the schema's, and so is a
// reason for a rule the block does not turn off, and an off with no reason inside our tiers.
func goShape() *schemaNode {
	severity := &schemaNode{Type: "string", Enum: settings.SeveritySpellings}
	setting := &schemaNode{
		Description: "A severity, or [severity, options] with one options object.",
		OneOf: []*schemaNode{
			severity,
			{
				Type:        "array",
				MinItems:    1,
				MaxItems:    2,
				PrefixItems: []*schemaNode{severity, {Type: "object"}},
			},
		},
	}
	return &schemaNode{
		Type:                 "object",
		AdditionalProperties: noAdditionalProperties,
		// The engine refuses a go block with no rules, since it reads as configured and configures nothing.
		Required: []string{"rules"},
		Properties: map[string]*schemaNode{
			"rules": {
				Type:                 "object",
				Description:          "Each cohere-go/<rule> the Go engine runs, and its severity.",
				AdditionalProperties: &additionalProperties{schema: setting},
			},
			"departures": {
				Type: "object",
				Description: "For each cohere-go/<rule> whose options loosen it past the house's value, why, in a " +
					"sentence. The engine refuses a loosening option with no departure, and a departure for a rule " +
					"whose options loosen nothing.",
				AdditionalProperties: &additionalProperties{
					schema: &schemaNode{Type: "string", Pattern: goReasonPattern},
				},
			},
			"reasons": {
				Type: "object",
				Description: "For each cohere-go/<rule> this block turns off, why, in a sentence. Required in a " +
					"module of ours, whose path is under " + our_tiers.ModulePrefix +
					", and optional outside it. Coverage " +
					"prints the reason beside the rule. An entry for a rule the block does not turn off is refused.",
				AdditionalProperties: &additionalProperties{
					schema: &schemaNode{Type: "string", Pattern: goReasonPattern},
				},
			},
			"targets": {
				Type: "array",
				Description: "Platforms, each GOOS/GOARCH as go tool dist list spells it, checked beside the ones " +
					"the project's files name in their build constraints and suffixes and the host's. They only " +
					"add: a " +
					"file whose constraint only rules platforms out, as !linux && !darwin, is checked under a target " +
					"that compiles it, and with none it is named as a gap. A pair the toolchain does not target is " +
					"refused at load.",
				Items: &schemaNode{Type: "string", Pattern: settings.TargetPattern},
			},
		},
	}
}

// swiftSeverities are the severities the Swift engine accepts, as written: its RuleConfiguration.Severity,
// whose raw values are compared exactly. Fewer than the TypeScript loader's, so the block has its own
// rather than the shared severity definition, which would complete a "warn" the engine refuses.
var swiftSeverities = []string{"error", "warning", "off"}

// swiftShape is what the Swift engine reads from its block (RuleConfiguration.parse): a rules object, each
// value a severity or an array of a severity and at most one options object, and nothing else, since the
// engine refuses a block with no rules and a format key in it. Which options a rule reads, and their limits,
// stay the engine's (RuleConfiguration.optionsRead), which refuses by name any it does not read, so the
// schema does not copy them and cannot drift from them.
func swiftShape() *schemaNode {
	severity := &schemaNode{Type: "string", Enum: swiftSeverities}
	return &schemaNode{
		Type:                 "object",
		AdditionalProperties: noAdditionalProperties,
		Required:             []string{"rules"},
		Properties: map[string]*schemaNode{
			"rules": {
				Type: "object",
				Description: "Each Swift rule's severity, or an array of its severity and, for a rule that reads " +
					"options, an object of them.",
				AdditionalProperties: &additionalProperties{schema: &schemaNode{
					OneOf: []*schemaNode{
						severity,
						{
							Type:        "array",
							MinItems:    1,
							MaxItems:    2,
							PrefixItems: []*schemaNode{severity, {Type: "object"}},
						},
					},
				}},
			},
		},
	}
}

func formatShape() *schemaNode {
	properties := map[string]*schemaNode{}
	for _, key := range format_options.BlockKeys {
		shape := &schemaNode{Description: formatDescriptions[key.Name]}
		switch key.Type {
		case "array":
			shape.Type = "array"
			shape.Items = &schemaNode{Type: "string"}
		case "integer":
			minimum := 0
			shape.Type = "integer"
			shape.Minimum = &minimum
		default:
			shape.Type = key.Type
		}
		if len(key.Enum) > 0 {
			shape.Enum = key.Enum
		}
		properties[key.Name] = shape
	}
	return &schemaNode{Type: "object", AdditionalProperties: noAdditionalProperties, Properties: properties}
}

// definitions holds the shapes several keys share: a rule's setting, the rules object, and an override.
func definitions(ruleNames []string) (map[string]*schemaNode, error) {
	severity := &schemaNode{
		Type:        "string",
		Pattern:     severityPattern(),
		Description: "One of " + strings.Join(configuration.SeveritySpellings, ", ") + ", in any case.",
	}
	ruleProperties := map[string]*schemaNode{}
	for _, name := range ruleNames {
		ruleProperties[name] = &schemaNode{Reference: "#/$defs/ruleSetting"}
	}
	overrideProperties := map[string]*schemaNode{}
	for _, field := range configuration.OverrideFields() {
		shape, typeShapeError := typeShape("overrides."+field.Name, field.Field)
		if typeShapeError != nil {
			return nil, typeShapeError
		}
		description, described := overrideDescriptions[field.Name]
		if !described {
			return nil, fmt.Errorf(
				"settings_schema: an override accepts %q and nothing describes it; add it to overrideDescriptions",
				field.Name,
			)
		}
		shape.Description = description
		overrideProperties[field.Name] = shape
	}
	return map[string]*schemaNode{
		"severity": severity,
		"ruleSetting": {
			Description: "A severity, or [severity, ...options] for a rule that takes options.",
			OneOf: []*schemaNode{
				{Reference: "#/$defs/severity"},
				{
					Type:        "array",
					MinItems:    1,
					PrefixItems: []*schemaNode{{Reference: "#/$defs/severity"}},
				},
			},
		},
		"rules": {
			Type: "object",
			Description: "Every rule cohere implements is listed for completion. A name it does not implement " +
				"is accepted and counted as a decision about that rule, so it is not an error here either.",
			Properties:           ruleProperties,
			AdditionalProperties: &additionalProperties{schema: &schemaNode{Reference: "#/$defs/ruleSetting"}},
		},
		"override": {
			Type:                 "object",
			AdditionalProperties: noAdditionalProperties,
			Properties:           overrideProperties,
		},
	}, nil
}

// severityPattern matches every spelling parseSeverity accepts, in any case.
func severityPattern() string {
	var spellings []string
	for _, spelling := range configuration.SeveritySpellings {
		var pattern strings.Builder
		for _, character := range spelling {
			pattern.WriteString("[" + strings.ToLower(string(character)) + strings.ToUpper(string(character)) + "]")
		}
		spellings = append(spellings, pattern.String())
	}
	return "^(" + strings.Join(spellings, "|") + ")$"
}

// encode writes a schema with sorted keys and four-space indentation, so a regeneration diffs cleanly. Build then
// lays it out in the house format, which keeps an object expanded where its source was, so the indentation is what
// keeps every object expanded as the committed file has it.
func encode(schema *schemaNode) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	if encodeError := encoder.Encode(schema); encodeError != nil {
		return nil, encodeError
	}
	return buffer.Bytes(), nil
}

// SchemaKeys returns a generated schema's top-level property names, sorted, for the tests.
func SchemaKeys(encoded []byte) ([]string, error) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if decodeError := json.Unmarshal(encoded, &schema); decodeError != nil {
		return nil, decodeError
	}
	keys := make([]string, 0, len(schema.Properties))
	for key := range schema.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}
