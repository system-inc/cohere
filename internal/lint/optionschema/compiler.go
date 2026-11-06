// Package optionschema holds every ported rule's upstream options schema and checks a config's options
// against it, so cohere accepts exactly the option shapes ESLint accepts and refuses the rest (#pd2chkx).
//
// The schemas are each plugin's own `meta.schema`, normalized as ESLint's getRuleOptionsSchema normalizes
// them and written by internal/lint/tools/option_schemas/extract.mjs into schemas.json, which is embedded.
// The verdict is ajv 6's as ESLint 10 configures it, and TestValidatorAgreesWithAjv holds this package to
// it in both directions over every sample the tool wrote.
//
// It is a subset of draft-04 rather than all of it, and the subset is exactly what the 384 schemas use:
// type, const, enum, properties, additionalProperties, patternProperties, required, items (one schema or a
// tuple), additionalItems, minItems, maxItems, uniqueItems, minimum, maximum, minLength, maxLength,
// pattern, anyOf, oneOf, allOf, not and $ref within the rule's own schema. One of ajv's keywords outside
// the subset refuses to compile, so a regenerated schema that starts using one fails a test instead of
// being read as permissive.
package optionschema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"
)

//go:embed schemas.json
var schemasFile []byte

// file is schemas.json's shape. Each rule's schema stays raw until a config asks for that rule.
type file struct {
	GeneratedFrom map[string]string          `json:"generatedFrom"`
	Rules         map[string]json.RawMessage `json:"rules"`
}

var (
	loadOnce  sync.Once
	loaded    file
	loadError error

	compiledMutex sync.Mutex
	compiled      = map[string]*Schema{}
)

func load() (file, error) {
	loadOnce.Do(func() {
		loadError = json.Unmarshal(schemasFile, &loaded)
	})
	return loaded, loadError
}

// GeneratedFrom is the version of every package the embedded schemas were read from.
func GeneratedFrom() map[string]string {
	// Ignored: a failed load leaves an empty file, so this reads as no versions, and For returns the error
	// to the config load that needs the schemas.
	schemas, _ := load()
	return schemas.GeneratedFrom
}

// RuleNames is every rule the embedded schemas cover, sorted.
func RuleNames() []string {
	// Ignored: a failed load leaves an empty file, so this reads as no rules, and For returns the error to
	// the config load that needs the schemas.
	schemas, _ := load()
	names := make([]string, 0, len(schemas.Rules))
	for name := range schemas.Rules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// For returns the compiled schema for a rule, or nil and false when upstream has none for it: either the
// rule is not a port, or its upstream opts out of validation with `schema: false`.
func For(ruleName string) (*Schema, bool, error) {
	schemas, loadError := load()
	if loadError != nil {
		return nil, false, fmt.Errorf("reading the embedded option schemas: %w", loadError)
	}
	raw, present := schemas.Rules[ruleName]
	// Go whitespace: raw JSON bytes of an embedded schema, whose whitespace is the same in both sets.
	if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("false")) {
		return nil, false, nil
	}

	compiledMutex.Lock()
	defer compiledMutex.Unlock()
	if schema, done := compiled[ruleName]; done {
		return schema, true, nil
	}
	schema, compileError := Compile(raw)
	if compileError != nil {
		return nil, false, fmt.Errorf("compiling %s's upstream option schema: %w", ruleName, compileError)
	}
	compiled[ruleName] = schema
	return schema, true, nil
}

// node is one schema object, with only the keywords this package implements.
type node struct {
	pointer string

	types      []string
	constant   JsonValue
	hasConst   bool
	enumValues []JsonValue
	hasEnum    bool

	properties           map[string]*node
	patternProperties    []patternProperty
	additionalProperties *node
	noAdditional         bool
	required             []string

	items             *node
	tupleItems        []*node
	additionalItems   *node
	noAdditionalItems bool
	minItems          *int
	maxItems          *int
	uniqueItems       bool

	minimum       *float64
	maximum       *float64
	minLength     *int
	maxLength     *int
	pattern       *esregexp.RegExp
	patternSource string

	anyOf []*node
	oneOf []*node
	allOf []*node
	not   *node

	// reference is a $ref, and referenceTarget the schema it lands on. ajv 6 ignores every keyword
	// beside a $ref (its extendRefs option defaults to "ignore"), so a node with one checks only that.
	reference       string
	referenceTarget *node

	always bool // true schema, or {}
	never  bool // false schema
}

type patternProperty struct {
	source  string
	pattern *esregexp.RegExp
	schema  *node
}

// unimplemented are ajv 6's validation keywords outside this package's subset. A schema using one
// refuses to compile, because reading it as absent would accept what ajv refuses.
//
// Every other name is ignored, as ajv 6 ignores a keyword it does not know (strictKeywords is off in
// ESLint's configuration). That covers documentation (description, title, default, $defs) and plugins'
// own mistakes: exhaustive-deps' schema writes enableDangerousAutofixThisMayCauseInfiniteLoops beside
// `properties` instead of inside it, so ajv validates nothing there, and neither does this.
var unimplemented = map[string]bool{
	"contains": true, "dependencies": true, "exclusiveMaximum": true, "exclusiveMinimum": true, "format": true,
	"if": true, "then": true, "else": true, "maxProperties": true, "minProperties": true, "multipleOf": true,
	"propertyNames": true, "$data": true,
}

// Compile turns a schema's JSON into a Schema.
func Compile(raw []byte) (*Schema, error) {
	// A decoder reads the schema's first value and ignores any text after it, as it always has, and its syntax
	// error is the one this has always returned. The value it read is already checked JSON.
	var first json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decodeError := decoder.Decode(&first); decodeError != nil {
		return nil, decodeError
	}
	document, parseError := ParseJsonValue(first)
	if parseError != nil {
		return nil, parseError
	}
	compiler := &compiler{document: document, byPointer: map[string]*node{}}
	root, compileError := compiler.compile(document, "#")
	if compileError != nil {
		return nil, compileError
	}
	if resolveReferencesError := compiler.resolveReferences(); resolveReferencesError != nil {
		return nil, resolveReferencesError
	}
	return &Schema{root: root}, nil
}

type compiler struct {
	document  JsonValue
	byPointer map[string]*node
	pending   []*node
}

func (compiler *compiler) compile(value JsonValue, pointer string) (*node, error) {
	compiled := &node{pointer: pointer}
	compiler.byPointer[pointer] = compiled
	switch typed := value.(type) {
	case JsonBoolean:
		compiled.always, compiled.never = bool(typed), !bool(typed)
		return compiled, nil
	case JsonObject:
		if len(typed) == 0 {
			compiled.always = true
			return compiled, nil
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if keywordError := compiler.keyword(compiled, key, typed[key], pointer); keywordError != nil {
				return nil, fmt.Errorf("%s/%s: %w", pointer, key, keywordError)
			}
		}
		// Nested definitions are compiled so a $ref can land on them by pointer.
		for _, definitions := range []string{"$defs", "definitions"} {
			if entries, isObject := typed[definitions].(JsonObject); isObject {
				for name, entry := range entries {
					if _, compileError := compiler.compile(
						entry,
						pointer+"/"+definitions+"/"+escapePointer(name),
					); compileError != nil {
						return nil, compileError
					}
				}
			}
		}
		return compiled, nil
	default:
		// No embedded schema reaches this (TestEveryEmbeddedSchemaCompiles): it is for a regenerated one.
		return nil, fmt.Errorf("a schema must be an object or a boolean, not %s", shapeName(value))
	}
}

// shapeName names a value's JSON shape in words, as a refusal reads it: "not an array".
func shapeName(value JsonValue) string {
	switch value.(type) {
	case JsonNull:
		return "null"
	case JsonBoolean:
		return "a boolean"
	case JsonNumber:
		return "a number"
	case JsonString:
		return "a string"
	case JsonArray:
		return "an array"
	case JsonObject:
		return "an object"
	}
	// Only a nil interface lands here, which ParseJsonValue never returns.
	return "no JSON value"
}

// keyword compiles one keyword of a schema object into the node that object compiles to.
//
// Mutates: compiled each keyword fills its own fields of the node compile is building, one call per key
func (compiler *compiler) keyword(compiled *node, key string, value JsonValue, pointer string) error {
	child := func(name string, value JsonValue) (*node, error) {
		return compiler.compile(value, pointer+"/"+name)
	}
	children := func(name string, value JsonValue) ([]*node, error) {
		list, isList := value.(JsonArray)
		if !isList {
			return nil, fmt.Errorf("%s must be a list of schemas", name)
		}
		nodes := make([]*node, len(list))
		for index, item := range list {
			compiledItem, compileError := compiler.compile(item, pointer+"/"+name+"/"+strconv.Itoa(index))
			if compileError != nil {
				return nil, compileError
			}
			nodes[index] = compiledItem
		}
		return nodes, nil
	}
	var keywordError error
	switch key {
	case "type":
		switch typed := value.(type) {
		case JsonString:
			compiled.types = []string{string(typed)}
		case JsonArray:
			for _, item := range typed {
				name, isString := item.(JsonString)
				if !isString {
					return fmt.Errorf("type lists only names")
				}
				compiled.types = append(compiled.types, string(name))
			}
		default:
			return fmt.Errorf("type must be a name or a list of names")
		}
		for _, name := range compiled.types {
			switch name {
			case "string", "number", "integer", "boolean", "object", "array", "null":
			default:
				return fmt.Errorf("unknown type %q", name)
			}
		}
	case "const":
		compiled.constant, compiled.hasConst = value, true
	case "enum":
		list, isList := value.(JsonArray)
		if !isList {
			return fmt.Errorf("enum must be a list")
		}
		compiled.enumValues, compiled.hasEnum = list, true
	case "properties":
		entries, isObject := value.(JsonObject)
		if !isObject {
			return fmt.Errorf("properties must be an object")
		}
		compiled.properties = make(map[string]*node, len(entries))
		for name, entry := range entries {
			if compiled.properties[name], keywordError = compiler.compile(
				entry,
				pointer+"/properties/"+escapePointer(name),
			); keywordError != nil {
				return keywordError
			}
		}
	case "patternProperties":
		entries, isObject := value.(JsonObject)
		if !isObject {
			return fmt.Errorf("patternProperties must be an object")
		}
		sources := make([]string, 0, len(entries))
		for source := range entries {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			pattern, compileError := esregexp.Compile(source, "")
			if compileError != nil {
				return compileError
			}
			schema, compileError := compiler.compile(
				entries[source],
				pointer+"/patternProperties/"+escapePointer(source),
			)
			if compileError != nil {
				return compileError
			}
			compiled.patternProperties = append(
				compiled.patternProperties,
				patternProperty{source: source, pattern: pattern, schema: schema},
			)
		}
	case "additionalProperties":
		if allowed, isBool := value.(JsonBoolean); isBool {
			compiled.noAdditional = !bool(allowed)
			return nil
		}
		compiled.additionalProperties, keywordError = child(key, value)
	case "required":
		list, isList := value.(JsonArray)
		if !isList {
			return fmt.Errorf("required must be a list")
		}
		for _, item := range list {
			name, isString := item.(JsonString)
			if !isString {
				return fmt.Errorf("required lists only names")
			}
			compiled.required = append(compiled.required, string(name))
		}
	case "items":
		if _, isList := value.(JsonArray); isList {
			var childrenError error
			compiled.tupleItems, childrenError = children(key, value)
			return childrenError
		}
		compiled.items, keywordError = child(key, value)
	case "additionalItems":
		if allowed, isBool := value.(JsonBoolean); isBool {
			compiled.noAdditionalItems = !bool(allowed)
			return nil
		}
		compiled.additionalItems, keywordError = child(key, value)
	case "minItems":
		compiled.minItems, keywordError = count(value)
	case "maxItems":
		compiled.maxItems, keywordError = count(value)
	case "minLength":
		compiled.minLength, keywordError = count(value)
	case "maxLength":
		compiled.maxLength, keywordError = count(value)
	case "uniqueItems":
		unique, isBool := value.(JsonBoolean)
		if !isBool {
			return fmt.Errorf("uniqueItems must be a boolean")
		}
		compiled.uniqueItems = bool(unique)
	case "minimum":
		compiled.minimum, keywordError = number(value)
	case "maximum":
		compiled.maximum, keywordError = number(value)
	case "pattern":
		source, isString := value.(JsonString)
		if !isString {
			return fmt.Errorf("pattern must be a string")
		}
		// ajv 6 compiles a pattern as `new RegExp(pattern)`, with no flags.
		compiled.pattern, keywordError = esregexp.Compile(string(source), "")
		compiled.patternSource = string(source)
	case "anyOf":
		compiled.anyOf, keywordError = children(key, value)
	case "oneOf":
		compiled.oneOf, keywordError = children(key, value)
	case "allOf":
		compiled.allOf, keywordError = children(key, value)
	case "not":
		compiled.not, keywordError = child(key, value)
	case "$ref":
		reference, isString := value.(JsonString)
		if !isString {
			return fmt.Errorf("$ref must be a string")
		}
		compiled.reference = string(reference)
		compiler.pending = append(compiler.pending, compiled)
	default:
		if unimplemented[key] {
			return fmt.Errorf("keyword %q is outside the subset this package implements", key)
		}
	}
	return keywordError
}

// resolveReferences checks that every $ref lands on a schema in the same document. A $ref is followed at
// validation time, so a schema that refers to itself does not recurse while compiling.
func (compiler *compiler) resolveReferences() error {
	for _, referring := range compiler.pending {
		if _, found := compiler.byPointer[referring.reference]; !found {
			target, pointerTargetError := pointerTarget(compiler.document, referring.reference)
			if pointerTargetError != nil {
				return pointerTargetError
			}
			if _, compileError := compiler.compile(target, referring.reference); compileError != nil {
				return compileError
			}
		}
	}
	for _, referring := range compiler.pending {
		referring.referenceTarget = compiler.byPointer[referring.reference]
	}
	return nil
}
