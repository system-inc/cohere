package typescript

import (
	"encoding/json"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// noDeprecatedRawSpecifier is one allowlist entry, in either of its two wire forms.
type noDeprecatedRawSpecifier struct {
	inline string

	From    string
	Name    []string
	Path    string
	Package string
}

// UnmarshalJSON accepts a bare string or the specifier object, and `name` as a string or an array.
//
// An unrecognized `from` is dropped rather than erroring, matching `only-throw-error` and
// `no-floating-promises`: the specifier then matches nothing, whereas keeping it would silently mean
// `file`, which is the zero value of the enum.
func (specifier *noDeprecatedRawSpecifier) UnmarshalJSON(raw []byte) error {
	var asString string
	if decodeError := json.Unmarshal(raw, &asString); decodeError == nil {
		*specifier = noDeprecatedRawSpecifier{inline: asString}
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

	*specifier = noDeprecatedRawSpecifier{From: object.From, Path: object.Path, Package: object.Package}

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
