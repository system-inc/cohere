package typescript

import (
	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// NoDeprecatedOptions is the rule's one option: an allowlist of things whose deprecation is
// accepted, so a codebase can keep using a deprecated API it has decided to keep using.
//
// The default is an empty allowlist, which is the zero value, so an absent configuration needs no
// special handling. That is worth stating because it is not true of every rule in this family:
// `no-base-to-string` defaults to four builtin type names and a decoder returning the zero value
// there would change behaviour.
type NoDeprecatedOptions struct {
	// Allow are specifier objects: a name plus where it comes from.
	Allow []type_checking.TypeOrValueSpecifier
	// AllowInline are the bare-string entries, matched against a type's own name.
	AllowInline []string
}

// noDeprecatedRawOptions is the wire shape.
//
// cohere's config layer strips ESLint's `[severity, options]` tuple and stores only the second
// element, so what arrives here is the bare object rather than upstream's one-element array.
type noDeprecatedRawOptions struct {
	Allow []noDeprecatedRawSpecifier `json:"allow"`
}

// DecodeNoDeprecatedOptions maps upstream's JSON onto the struct the rule reads.
//
// Hand-written rather than `rule.DecodeOptionsInto` because `allow` is one heterogeneous array whose
// two forms land in two different fields, and because `from` is an enum rather than a string.
func DecodeNoDeprecatedOptions(raw []byte) (NoDeprecatedOptions, error) {
	decoded, decodeOptionsError := rule.DecodeOptionsInto[noDeprecatedRawOptions]()(raw)
	if decodeOptionsError != nil {
		return NoDeprecatedOptions{}, decodeOptionsError
	}

	options := NoDeprecatedOptions{}
	for _, entry := range decoded.Allow {
		if entry.inline != "" {
			options.AllowInline = append(options.AllowInline, entry.inline)
			continue
		}

		specifier := type_checking.TypeOrValueSpecifier{Name: entry.Name, Path: entry.Path, Package: entry.Package}
		switch entry.From {
		case "file":
			specifier.From = type_checking.TypeOrValueSpecifierFromFile
		case "lib":
			specifier.From = type_checking.TypeOrValueSpecifierFromLib
		case "package":
			specifier.From = type_checking.TypeOrValueSpecifierFromPackage
		default:
			continue
		}
		options.Allow = append(options.Allow, specifier)
	}

	return options, nil
}
