package tailwind

import "encoding/json"

// tsconfigFile is one tsconfig read as JSON, its top-level keys kept raw.
type tsconfigFile map[string]json.RawMessage

func (file tsconfigFile) compilerOptions() map[string]json.RawMessage {
	options := map[string]json.RawMessage{}
	// Ignored: an absent compilerOptions has no options, and one that is not an object is dropped whole, as
	// tsoptions' convertOptionsFromJson drops it (TypeScript/tsc/internal/tsoptions/tsconfigparsing.go).
	_ = json.Unmarshal(file["compilerOptions"], &options)
	return options
}
