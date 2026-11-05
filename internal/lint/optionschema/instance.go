package optionschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Extension is a shape cohere accepts on purpose beyond upstream's schema: one key of one option
// element, removed before the element is checked and left for the rule's own decoder to read.
type Extension struct {
	Rule string
	// Element counts as a config author does: 1 is the first element after the severity.
	Element int
	Key     string
	Reason  string
}

// Extensions are every shape cohere accepts that upstream's schema refuses. Each is a decision, so the
// list's length is asserted exactly (TestExtensionsAreExactlyTheDeclaredOnes) and adding one means
// changing that number on purpose (#pd2chkx condition 6).
var Extensions = []Extension{
	{
		Rule:    "boundaries/dependencies",
		Element: 1,
		Key:     "elements",
		Reason: "upstream reads its element descriptors from settings[\"boundaries/elements\"] beside the rule, " +
			"and cohere carries no boundaries settings, so they are written in the rule's own options",
	},
}

// Check validates a rule's option elements, the list after the severity, against upstream's schema. A
// rule with no schema here, either not a port or one whose upstream opts out, passes unchecked.
func Check(ruleName string, elements []json.RawMessage) error {
	schema, found, optionSchemaForError := For(ruleName)
	if optionSchemaForError != nil || !found {
		return optionSchemaForError
	}
	return schema.Validate(withoutExtensions(ruleName, elements))
}

// withoutExtensions removes each declared extension key from its element, leaving the rest to be
// checked as upstream wrote its schema.
func withoutExtensions(ruleName string, elements []json.RawMessage) []json.RawMessage {
	var stripped []json.RawMessage
	for _, extension := range Extensions {
		if extension.Rule != ruleName || extension.Element < 1 || extension.Element > len(elements) {
			continue
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(elements[extension.Element-1], &object) != nil {
			continue
		}
		if _, present := object[extension.Key]; !present {
			continue
		}
		delete(object, extension.Key)
		encoded, encodeError := json.Marshal(object)
		if encodeError != nil {
			continue
		}
		if stripped == nil {
			stripped = append([]json.RawMessage(nil), elements...)
		}
		stripped[extension.Element-1] = encoded
	}
	if stripped == nil {
		return elements
	}
	return stripped
}

func validate(schema *node, value JsonValue, path string) *Violation {
	if schema.referenceTarget != nil {
		return validate(schema.referenceTarget, value, path)
	}
	if schema.always {
		return nil
	}
	if schema.never {
		return &Violation{Path: pathOrRoot(path), Expected: "nothing at all here"}
	}

	if len(schema.types) > 0 && !hasAnyType(value, schema.types) {
		return &Violation{
			Path:     pathOrRoot(path),
			Expected: fmt.Sprintf("%s, got %s", typeList(schema.types), describe(value)),
		}
	}
	if schema.hasConst && !equal(value, schema.constant) {
		return &Violation{
			Path:     pathOrRoot(path),
			Expected: fmt.Sprintf("%s, got %s", encode(schema.constant), describe(value)),
		}
	}
	if schema.hasEnum {
		matched := false
		for _, candidate := range schema.enumValues {
			if equal(value, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			listed := make([]string, len(schema.enumValues))
			for index, candidate := range schema.enumValues {
				listed[index] = encode(candidate)
			}
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("one of %s, got %s", strings.Join(listed, ", "), describe(value)),
			}
		}
	}

	switch typed := value.(type) {
	case JsonNumber:
		// Ignored: the decoder only yields a valid number literal, and one past float64's range comes back
		// as an infinity of its sign with the range error, which still compares truthfully to both bounds.
		number, _ := json.Number(typed).Float64()
		if schema.minimum != nil && number < *schema.minimum {
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("a number of at least %s, got %s", formatNumber(*schema.minimum), typed),
			}
		}
		if schema.maximum != nil && number > *schema.maximum {
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("a number of at most %s, got %s", formatNumber(*schema.maximum), typed),
			}
		}
	case JsonString:
		length := utf8.RuneCountInString(string(typed))
		if schema.minLength != nil && length < *schema.minLength {
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("a string of at least %d characters, got %s", *schema.minLength, describe(value)),
			}
		}
		if schema.maxLength != nil && length > *schema.maxLength {
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("a string of at most %d characters, got %s", *schema.maxLength, describe(value)),
			}
		}
		// A match that runs past esregexp's time bound counts as a match: this check may only refuse
		// what ajv refuses, and a refusal stops the whole run.
		if schema.pattern != nil && !schema.pattern.TestOrTimeout(string(typed)) {
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("a string matching /%s/, got %s", schema.patternSource, describe(value)),
			}
		}
	case JsonArray:
		if violation := validateArray(schema, typed, path); violation != nil {
			return violation
		}
	case JsonObject:
		if violation := validateObject(schema, typed, path); violation != nil {
			return violation
		}
	}

	for _, branch := range schema.allOf {
		if violation := validate(branch, value, path); violation != nil {
			return violation
		}
	}
	if len(schema.anyOf) > 0 {
		var first *Violation
		matched := false
		for _, branch := range schema.anyOf {
			violation := validate(branch, value, path)
			if violation == nil {
				matched = true
				break
			}
			if first == nil {
				first = violation
			}
		}
		if !matched {
			return &Violation{Path: pathOrRoot(path), Expected: fmt.Sprintf(
				"one of %d shapes, and matches none (the first: %s)",
				len(schema.anyOf),
				first.Error(),
			)}
		}
	}
	if len(schema.oneOf) > 0 {
		matches := 0
		var first *Violation
		for _, branch := range schema.oneOf {
			violation := validate(branch, value, path)
			if violation == nil {
				matches++
			} else if first == nil {
				first = violation
			}
		}
		switch {
		case matches == 0:
			return &Violation{Path: pathOrRoot(path), Expected: fmt.Sprintf(
				"exactly one of %d shapes, and matches none (the first: %s)",
				len(schema.oneOf),
				first.Error(),
			)}
		case matches > 1:
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("exactly one of %d shapes, and matches %d of them", len(schema.oneOf), matches),
			}
		}
	}
	if schema.not != nil && validate(schema.not, value, path) == nil {
		return &Violation{Path: pathOrRoot(path), Expected: fmt.Sprintf("anything but %s", describe(value))}
	}
	return nil
}

func validateArray(schema *node, list JsonArray, path string) *Violation {
	if schema.minItems != nil && len(list) < *schema.minItems {
		return &Violation{
			Path:     pathOrRoot(path),
			Expected: fmt.Sprintf("at least %d items, got %d", *schema.minItems, len(list)),
		}
	}
	if schema.maxItems != nil && len(list) > *schema.maxItems {
		return &Violation{
			Path:     pathOrRoot(path),
			Expected: fmt.Sprintf("at most %d items, got %d", *schema.maxItems, len(list)),
		}
	}
	if schema.uniqueItems {
		for later := 1; later < len(list); later++ {
			for earlier := 0; earlier < later; earlier++ {
				if equal(list[earlier], list[later]) {
					return &Violation{Path: pathOrRoot(path), Expected: fmt.Sprintf(
						"unique items, and items %d and %d are both %s",
						earlier,
						later,
						encode(list[later]),
					)}
				}
			}
		}
	}
	for index, item := range list {
		itemPath := path + "[" + strconv.Itoa(index) + "]"
		var itemSchema *node
		switch {
		case schema.tupleItems != nil && index < len(schema.tupleItems):
			itemSchema = schema.tupleItems[index]
		case schema.tupleItems != nil && schema.noAdditionalItems:
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("at most %d items, got %d", len(schema.tupleItems), len(list)),
			}
		case schema.tupleItems != nil:
			itemSchema = schema.additionalItems
		default:
			itemSchema = schema.items
		}
		if itemSchema == nil {
			continue
		}
		if violation := validate(itemSchema, item, itemPath); violation != nil {
			return violation
		}
	}
	return nil
}

func validateObject(schema *node, object JsonObject, path string) *Violation {
	for _, name := range schema.required {
		if _, present := object[name]; !present {
			return &Violation{Path: pathOrRoot(path), Expected: fmt.Sprintf("the key %q, which is required", name)}
		}
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		keyPath := path + "." + key
		declared := false
		if property, found := schema.properties[key]; found {
			declared = true
			if violation := validate(property, object[key], keyPath); violation != nil {
				return violation
			}
		}
		for _, patterned := range schema.patternProperties {
			if patterned.pattern.TestOrTimeout(key) {
				declared = true
				if violation := validate(patterned.schema, object[key], keyPath); violation != nil {
					return violation
				}
			}
		}
		if declared {
			continue
		}
		if schema.noAdditional {
			return &Violation{
				Path:     pathOrRoot(path),
				Expected: fmt.Sprintf("only its declared keys, and %q is not one", key),
			}
		}
		if schema.additionalProperties != nil {
			if violation := validate(schema.additionalProperties, object[key], keyPath); violation != nil {
				return violation
			}
		}
	}
	return nil
}

// hasAnyType is ajv's type check: "integer" is a number with no fractional part.
func hasAnyType(value JsonValue, types []string) bool {
	for _, name := range types {
		switch name {
		case "null":
			if _, is := value.(JsonNull); is {
				return true
			}
		case "boolean":
			if _, is := value.(JsonBoolean); is {
				return true
			}
		case "string":
			if _, is := value.(JsonString); is {
				return true
			}
		case "number":
			if _, is := value.(JsonNumber); is {
				return true
			}
		case "integer":
			if number, is := value.(JsonNumber); is {
				parsed, float64Error := json.Number(number).Float64()
				if float64Error == nil && !math.IsInf(parsed, 0) && parsed == math.Trunc(parsed) {
					return true
				}
			}
		case "array":
			if _, is := value.(JsonArray); is {
				return true
			}
		case "object":
			if _, is := value.(JsonObject); is {
				return true
			}
		}
	}
	return false
}

// equal is ajv's deep equality (fast-deep-equal): numbers by value, objects by key set and values.
func equal(left JsonValue, right JsonValue) bool {
	switch typedLeft := left.(type) {
	case JsonNull:
		_, is := right.(JsonNull)
		return is
	case JsonBoolean:
		typedRight, is := right.(JsonBoolean)
		return is && typedLeft == typedRight
	case JsonString:
		typedRight, is := right.(JsonString)
		return is && typedLeft == typedRight
	case JsonNumber:
		typedRight, is := right.(JsonNumber)
		if !is {
			return false
		}
		leftValue, leftError := json.Number(typedLeft).Float64()
		rightValue, rightError := json.Number(typedRight).Float64()
		return leftError == nil && rightError == nil && leftValue == rightValue
	case JsonArray:
		typedRight, is := right.(JsonArray)
		if !is || len(typedLeft) != len(typedRight) {
			return false
		}
		for index := range typedLeft {
			if !equal(typedLeft[index], typedRight[index]) {
				return false
			}
		}
		return true
	case JsonObject:
		typedRight, is := right.(JsonObject)
		if !is || len(typedLeft) != len(typedRight) {
			return false
		}
		for key, value := range typedLeft {
			other, present := typedRight[key]
			if !present || !equal(value, other) {
				return false
			}
		}
		return true
	}
	return false
}

func count(value JsonValue) (*int, error) {
	number, isNumber := value.(JsonNumber)
	if !isNumber {
		return nil, fmt.Errorf("must be a number")
	}
	parsed, parseError := strconv.Atoi(string(number))
	if parseError != nil || parsed < 0 {
		return nil, fmt.Errorf("must be a non-negative integer")
	}
	return &parsed, nil
}

func number(value JsonValue) (*float64, error) {
	typed, isNumber := value.(JsonNumber)
	if !isNumber {
		return nil, fmt.Errorf("must be a number")
	}
	parsed, float64Error := json.Number(typed).Float64()
	if float64Error != nil {
		return nil, float64Error
	}
	return &parsed, nil
}

func escapePointer(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
}

// pointerTarget follows a JSON pointer reference (`#/items/0/$defs/name`) through the document.
func pointerTarget(document JsonValue, reference string) (JsonValue, error) {
	if reference == "#" {
		return document, nil
	}
	rest, found := strings.CutPrefix(reference, "#/")
	if !found {
		return nil, fmt.Errorf("$ref %q is not a pointer into this schema", reference)
	}
	current := document
	for _, part := range strings.Split(rest, "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case JsonObject:
			next, present := typed[part]
			if !present {
				return nil, fmt.Errorf("$ref %q names %q, which is not there", reference, part)
			}
			current = next
		case JsonArray:
			index, parseError := strconv.Atoi(part)
			if parseError != nil || index < 0 || index >= len(typed) {
				return nil, fmt.Errorf("$ref %q names item %q, which is not there", reference, part)
			}
			current = typed[index]
		default:
			return nil, fmt.Errorf("$ref %q runs past a value at %q", reference, part)
		}
	}
	return current, nil
}

func pathOrRoot(path string) string {
	if path == "" {
		return "the option list"
	}
	return path
}

func typeList(types []string) string {
	if len(types) == 1 {
		return "a value of type " + types[0]
	}
	return "a value of type " + strings.Join(types[:len(types)-1], ", ") + " or " + types[len(types)-1]
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func encode(value JsonValue) string {
	var encoded bytes.Buffer
	if encodeError := writeJson(&encoded, value); encodeError != nil {
		return fmt.Sprint(value)
	}
	return encoded.String()
}

// writeJson writes a value as json.Marshal wrote the untyped value it was once decoded into: compact, an
// object's keys sorted, a number in its own spelling, and every string and key escaped by json.Marshal itself.
//
// Mutates: encoded the buffer each value is appended to, in order
func writeJson(encoded *bytes.Buffer, value JsonValue) error {
	switch typed := value.(type) {
	case JsonNull:
		encoded.WriteString("null")
	case JsonBoolean:
		encoded.WriteString(strconv.FormatBool(bool(typed)))
	case JsonNumber:
		return writeMarshaled(encoded, json.Number(typed))
	case JsonString:
		return writeMarshaled(encoded, string(typed))
	case JsonArray:
		encoded.WriteByte('[')
		for index, element := range typed {
			if index > 0 {
				encoded.WriteByte(',')
			}
			if writeError := writeJson(encoded, element); writeError != nil {
				return writeError
			}
		}
		encoded.WriteByte(']')
	case JsonObject:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		encoded.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				encoded.WriteByte(',')
			}
			if writeError := writeMarshaled(encoded, key); writeError != nil {
				return writeError
			}
			encoded.WriteByte(':')
			if writeError := writeJson(encoded, typed[key]); writeError != nil {
				return writeError
			}
		}
		encoded.WriteByte('}')
	default:
		return fmt.Errorf("writing JSON: %T is not a JSON value", value)
	}
	return nil
}

// writeMarshaled writes one number or string as json.Marshal writes it, so its escaping is exactly that.
//
// Mutates: encoded the buffer the marshaled text is appended to
func writeMarshaled[T json.Number | string](encoded *bytes.Buffer, value T) error {
	marshaled, marshalError := json.Marshal(value)
	if marshalError != nil {
		return fmt.Errorf("writing JSON: %w", marshalError)
	}
	encoded.Write(marshaled)
	return nil
}

func describe(value JsonValue) string {
	encoded := encode(value)
	if len(encoded) > 60 {
		encoded = encoded[:57] + "..."
	}
	return encoded
}
