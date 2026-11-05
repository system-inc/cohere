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
	loadOnce sync.Once
	loaded   file
	loadErr  error

	compiledMutex sync.Mutex
	compiled      = map[string]*Schema{}
)

func load() (file, error) {
	loadOnce.Do(func() {
		loadErr = json.Unmarshal(schemasFile, &loaded)
	})
	return loaded, loadErr
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
	schemas, err := load()
	if err != nil {
		return nil, false, fmt.Errorf("reading the embedded option schemas: %w", err)
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
	schema, err := Compile(raw)
	if err != nil {
		return nil, false, fmt.Errorf("compiling %s's upstream option schema: %w", ruleName, err)
	}
	compiled[ruleName] = schema
	return schema, true, nil
}

// Schema is one compiled schema: a node of the tree, with the root it resolves references against.
type Schema struct {
	root *node
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
	var document JsonValue
	// A decoder rather than json.Unmarshal, so text after the schema's first value is ignored, as it was.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decodeError := decoder.Decode(&document); decodeError != nil {
		return nil, decodeError
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
	switch value.Kind {
	case JsonKindBoolean:
		compiled.always, compiled.never = value.Boolean, !value.Boolean
		return compiled, nil
	case JsonKindObject:
		if len(value.Members) == 0 {
			compiled.always = true
			return compiled, nil
		}
		keys := make([]string, 0, len(value.Members))
		for key := range value.Members {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if keywordError := compiler.keyword(compiled, key, value.Members[key], pointer); keywordError != nil {
				return nil, fmt.Errorf("%s/%s: %w", pointer, key, keywordError)
			}
		}
		// Nested definitions are compiled so a $ref can land on them by pointer.
		for _, definitions := range []string{"$defs", "definitions"} {
			if entries, present := value.Members[definitions]; present && entries.Kind == JsonKindObject {
				for name, entry := range entries.Members {
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
		return nil, fmt.Errorf("a schema must be an object or a boolean, not %s", untypedGoName(value.Kind))
	}
}

// untypedGoName is the name `%T` printed for a value of this kind when the schema was decoded untyped, so a
// schema that is neither an object nor a boolean is refused in the words it always was.
func untypedGoName(kind JsonKind) string {
	switch kind {
	case JsonKindNull:
		return "<nil>"
	case JsonKindBoolean:
		return "bool"
	case JsonKindNumber:
		return "json.Number"
	case JsonKindString:
		return "string"
	case JsonKindArray:
		return "[]interface {}"
	case JsonKindObject:
		return "map[string]interface {}"
	}
	return string(kind)
}

// keyword compiles one keyword of a schema object into the node that object compiles to.
//
// Mutates: compiled each keyword fills its own fields of the node compile is building, one call per key
func (compiler *compiler) keyword(compiled *node, key string, value JsonValue, pointer string) error {
	child := func(name string, value JsonValue) (*node, error) {
		return compiler.compile(value, pointer+"/"+name)
	}
	children := func(name string, value JsonValue) ([]*node, error) {
		if value.Kind != JsonKindArray {
			return nil, fmt.Errorf("%s must be a list of schemas", name)
		}
		nodes := make([]*node, len(value.Elements))
		for index, item := range value.Elements {
			compiledItem, compileError := compiler.compile(item, pointer+"/"+name+"/"+strconv.Itoa(index))
			if compileError != nil {
				return nil, compileError
			}
			nodes[index] = compiledItem
		}
		return nodes, nil
	}
	var err error
	switch key {
	case "type":
		switch value.Kind {
		case JsonKindString:
			compiled.types = []string{value.Text}
		case JsonKindArray:
			for _, item := range value.Elements {
				if item.Kind != JsonKindString {
					return fmt.Errorf("type lists only names")
				}
				compiled.types = append(compiled.types, item.Text)
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
		if value.Kind != JsonKindArray {
			return fmt.Errorf("enum must be a list")
		}
		compiled.enumValues, compiled.hasEnum = value.Elements, true
	case "properties":
		if value.Kind != JsonKindObject {
			return fmt.Errorf("properties must be an object")
		}
		compiled.properties = make(map[string]*node, len(value.Members))
		for name, entry := range value.Members {
			if compiled.properties[name], err = compiler.compile(
				entry,
				pointer+"/properties/"+escapePointer(name),
			); err != nil {
				return err
			}
		}
	case "patternProperties":
		if value.Kind != JsonKindObject {
			return fmt.Errorf("patternProperties must be an object")
		}
		sources := make([]string, 0, len(value.Members))
		for source := range value.Members {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			pattern, compileErr := esregexp.Compile(source, "")
			if compileErr != nil {
				return compileErr
			}
			schema, compileErr := compiler.compile(
				value.Members[source],
				pointer+"/patternProperties/"+escapePointer(source),
			)
			if compileErr != nil {
				return compileErr
			}
			compiled.patternProperties = append(
				compiled.patternProperties,
				patternProperty{source: source, pattern: pattern, schema: schema},
			)
		}
	case "additionalProperties":
		if value.Kind == JsonKindBoolean {
			compiled.noAdditional = !value.Boolean
			return nil
		}
		compiled.additionalProperties, err = child(key, value)
	case "required":
		if value.Kind != JsonKindArray {
			return fmt.Errorf("required must be a list")
		}
		for _, item := range value.Elements {
			if item.Kind != JsonKindString {
				return fmt.Errorf("required lists only names")
			}
			compiled.required = append(compiled.required, item.Text)
		}
	case "items":
		if value.Kind == JsonKindArray {
			compiled.tupleItems, err = children(key, value)
			return err
		}
		compiled.items, err = child(key, value)
	case "additionalItems":
		if value.Kind == JsonKindBoolean {
			compiled.noAdditionalItems = !value.Boolean
			return nil
		}
		compiled.additionalItems, err = child(key, value)
	case "minItems":
		compiled.minItems, err = count(value)
	case "maxItems":
		compiled.maxItems, err = count(value)
	case "minLength":
		compiled.minLength, err = count(value)
	case "maxLength":
		compiled.maxLength, err = count(value)
	case "uniqueItems":
		if value.Kind != JsonKindBoolean {
			return fmt.Errorf("uniqueItems must be a boolean")
		}
		compiled.uniqueItems = value.Boolean
	case "minimum":
		compiled.minimum, err = number(value)
	case "maximum":
		compiled.maximum, err = number(value)
	case "pattern":
		if value.Kind != JsonKindString {
			return fmt.Errorf("pattern must be a string")
		}
		// ajv 6 compiles a pattern as `new RegExp(pattern)`, with no flags.
		compiled.pattern, err = esregexp.Compile(value.Text, "")
		compiled.patternSource = value.Text
	case "anyOf":
		compiled.anyOf, err = children(key, value)
	case "oneOf":
		compiled.oneOf, err = children(key, value)
	case "allOf":
		compiled.allOf, err = children(key, value)
	case "not":
		compiled.not, err = child(key, value)
	case "$ref":
		if value.Kind != JsonKindString {
			return fmt.Errorf("$ref must be a string")
		}
		compiled.reference = value.Text
		compiler.pending = append(compiler.pending, compiled)
	default:
		if unimplemented[key] {
			return fmt.Errorf("keyword %q is outside the subset this package implements", key)
		}
	}
	return err
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
