package typescript

import (
	"encoding/json"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// noFloatingPromisesRawSpecifier is one entry of either allowlist, in either of its two wire forms.
type noFloatingPromisesRawSpecifier struct {
	inline string

	From    string
	Name    []string
	Path    string
	Package string
}

// UnmarshalJSON accepts both wire forms: a bare string, or the object the schema describes.
//
// `name` is itself two shapes, a string or an array of strings, and it is required on the object
// form. An unrecognized `from` is left as the zero value rather than erroring, which matches how
// the rest of this config treats a value it cannot use: the specifier then matches nothing instead
// of failing the run.
func (specifier *noFloatingPromisesRawSpecifier) UnmarshalJSON(raw []byte) error {
	var asString string
	if decodeError := json.Unmarshal(raw, &asString); decodeError == nil {
		*specifier = noFloatingPromisesRawSpecifier{inline: asString}
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

	*specifier = noFloatingPromisesRawSpecifier{From: object.From, Path: object.Path, Package: object.Package}

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
