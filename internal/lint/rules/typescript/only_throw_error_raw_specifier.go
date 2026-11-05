package typescript

import (
	"encoding/json"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// onlyThrowErrorRawSpecifier is one entry of the allowlist, in either of its two wire forms.
type onlyThrowErrorRawSpecifier struct {
	inline string

	From    string
	Name    []string
	Path    string
	Package string
}

// UnmarshalJSON accepts both wire forms: a bare string, or the object the schema describes.
//
// `name` is itself two shapes, a string or an array of strings. An unrecognized `from` is dropped
// rather than erroring, which matches how `no-floating-promises` treats the same field: the
// specifier then matches nothing instead of failing the whole run, and dropping it says nothing
// rather than silently meaning `file`, which is the zero value.
func (specifier *onlyThrowErrorRawSpecifier) UnmarshalJSON(raw []byte) error {
	var asString string
	if decodeError := json.Unmarshal(raw, &asString); decodeError == nil {
		*specifier = onlyThrowErrorRawSpecifier{inline: asString}
		return nil
	}

	var object struct {
		From    string          `json:"from"`
		Name    json.RawMessage `json:"name"`
		Path    string          `json:"path"`
		Package string          `json:"package"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &object); unmarshalOptionsError != nil {
		return unmarshalOptionsError
	}

	*specifier = onlyThrowErrorRawSpecifier{From: object.From, Path: object.Path, Package: object.Package}

	var names []string
	if decodeError := json.Unmarshal(object.Name, &names); decodeError == nil {
		specifier.Name = names
		return nil
	}
	var name string
	if decodeError := json.Unmarshal(object.Name, &name); decodeError != nil {
		return decodeError
	}
	specifier.Name = []string{name}
	return nil
}
