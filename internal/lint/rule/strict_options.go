package rule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// UnmarshalOptions decodes a rule's option JSON into a struct, refusing any key the struct does not
// declare and any key spelled in a different case from its declaration.
//
// Go's `json.Unmarshal` does neither. It drops an unknown key without a word and matches keys
// case-insensitively, so `enforceForTsTypes` silently set `enforceForTSTypes` and `ignorePatern`
// silently set nothing. Upstream's schemas refuse both, and a config that loads clean while an
// option does nothing is the defect this whole options layer exists to end (#4n972g9, after
// #27dkbbw closed the same hole one level up, for whole option elements).
//
// `DisallowUnknownFields` covers unknown keys and does not cover case: a key that matches a field
// case-insensitively is not unknown to it. So the keys are also walked against the declared names,
// exactly, after decoding. A type with its own `UnmarshalJSON` is not descended into, because it
// decides its own shape; it is responsible for its own strictness.
//
// Case is enforced only for a field with a `json` tag, because only a tag declares the spelling. An
// untagged field reaches its config key through the case-insensitive match itself (`Ignore` reads
// `ignore`), and deriving upstream's spelling from a Go name would be a guess at every acronym
// (`URLPatterns` is `urlPatterns`, not `uRLPatterns`). Measured 2026-10-02: ahra's and phi's own
// configs reach untagged fields this way, so enforcing case on them would refuse working configs. An
// unknown key is refused either way.
func UnmarshalOptions[Options any](raw []byte, target *Options) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(target); decodeError != nil {
		return decodeError
	}

	// The whole input is checked once more, as one JSON value: Decode above reads only the first value, and
	// trailing text after it is refused here, as it always was.
	var document json.RawMessage
	if decodeError := json.Unmarshal(raw, &document); decodeError != nil {
		return decodeError
	}
	return checkKeyCase(document, reflect.TypeFor[*Options](), "")
}

var jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()

// checkKeyCase walks JSON beside the Go type it was decoded into and refuses an object key that only matched
// its field case-insensitively. The JSON stays raw and is decoded one level at a time, as an object or a list
// where the type expects one, so no part of it is held untyped; anything else has no keys to check.
func checkKeyCase(value json.RawMessage, valueType reflect.Type, path string) error {
	if valueType == nil {
		return nil
	}
	// baseType is valueType with its pointers walked off, so valueType still names what the caller passed.
	baseType := valueType
	for baseType.Kind() == reflect.Pointer {
		if baseType.Implements(jsonUnmarshalerType) {
			return nil
		}
		baseType = baseType.Elem()
	}
	if baseType.Implements(jsonUnmarshalerType) || reflect.PointerTo(baseType).Implements(jsonUnmarshalerType) {
		return nil
	}

	switch baseType.Kind() {
	case reflect.Struct:
		object, isObject := decodeObject(value)
		if !isObject {
			return nil
		}
		fields := declaredFields(baseType)
		for key, member := range object {
			field, exact := fields[key]
			if !exact {
				for declared, candidate := range fields {
					if !strings.EqualFold(declared, key) {
						continue
					}
					if candidate.tagged {
						return fmt.Errorf(
							"option %q is spelled %q: keys are matched exactly, as upstream's schema matches them",
							path+key,
							path+declared,
						)
					}
					field, exact = candidate, true
				}
				if !exact {
					continue
				}
			}
			if checkKeyCaseError := checkKeyCase(member, field.Type, path+key+"."); checkKeyCaseError != nil {
				return checkKeyCaseError
			}
		}
	case reflect.Slice, reflect.Array:
		var list []json.RawMessage
		if json.Unmarshal(value, &list) != nil {
			// Not a list, so it holds no keys to check, as a non-list never did.
			return nil
		}
		for index, element := range list {
			if checkKeyCaseError := checkKeyCase(
				element,
				baseType.Elem(),
				fmt.Sprintf("%s%d.", path, index),
			); checkKeyCaseError != nil {
				return checkKeyCaseError
			}
		}
	case reflect.Map:
		object, isObject := decodeObject(value)
		if !isObject {
			return nil
		}
		for key, member := range object {
			if checkKeyCaseError := checkKeyCase(member, baseType.Elem(), path+key+"."); checkKeyCaseError != nil {
				return checkKeyCaseError
			}
		}
	}
	return nil
}

// decodeObject reads value as a JSON object, its members left raw, and reports whether it is one. `null`
// decodes into a nil map without an error, and it is no object, which is why the map's nil-ness is checked.
func decodeObject(value json.RawMessage) (map[string]json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(value, &object) != nil || object == nil {
		return nil, false
	}
	return object, true
}

// declaredField is a struct field and whether a `json` tag declared its key's spelling.
type declaredField struct {
	reflect.StructField
	tagged bool
}

// declaredFields maps each JSON key a struct declares to its field, the way encoding/json names
// them: the tag's name, or the Go field name when the tag gives none, with an untagged embedded
// struct's fields promoted into its parent.
func declaredFields(structType reflect.Type) map[string]declaredField {
	fields := map[string]declaredField{}
	for index := range structType.NumField() {
		field := structType.Field(index)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if field.Anonymous && name == "" {
			embedded := field.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				for promotedName, promoted := range declaredFields(embedded) {
					if _, shadowed := fields[promotedName]; !shadowed {
						fields[promotedName] = promoted
					}
				}
				continue
			}
		}
		if !field.IsExported() {
			continue
		}
		tagged := name != ""
		if !tagged {
			name = field.Name
		}
		fields[name] = declaredField{StructField: field, tagged: tagged}
	}
	return fields
}
