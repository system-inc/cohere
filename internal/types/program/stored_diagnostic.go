package program

import (
	"reflect"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
)

// StoredDiagnostic is a compiler diagnostic in the file it belongs to, as the compiler's own build info
// stores one: location, code, category, message key and arguments or external text, chain and related
// information. NoFile marks a part the compiler gave no file.
type StoredDiagnostic struct {
	Pos, End           int
	Code               int32
	Category           int64
	MessageKey         string
	MessageArgs        []string
	Source             string
	MessageText        string
	ReportsUnnecessary bool
	ReportsDeprecated  bool
	SkippedOnNoEmit    bool
	NoFile             bool
	Chain              []StoredDiagnostic
	Related            []StoredDiagnostic
}

// diagnostic rebuilds the compiler diagnostic, the way the compiler rebuilds one from its build info.
func (stored StoredDiagnostic) diagnostic(sourceFile *ast.SourceFile) *ast.Diagnostic {
	file := sourceFile
	if stored.NoFile {
		file = nil
	}
	var chain []*ast.Diagnostic
	var related []*ast.Diagnostic
	for _, part := range stored.Chain {
		chain = append(chain, part.diagnostic(sourceFile))
	}
	for _, part := range stored.Related {
		related = append(related, part.diagnostic(sourceFile))
	}
	// The shim does not name the compiler's key and category types, so each is set through a value of its
	// own type, taken from a diagnostic's accessor.
	var zero ast.Diagnostic
	key, category := zero.MessageKey(), zero.Category()
	reflect.ValueOf(&key).Elem().SetString(stored.MessageKey)
	reflect.ValueOf(&category).Elem().SetInt(stored.Category)
	diagnostic := ast.NewDiagnosticFromSerialized(
		file,
		core.NewTextRange(stored.Pos, stored.End),
		stored.Code,
		category,
		key,
		stored.MessageArgs,
		chain,
		related,
		stored.ReportsUnnecessary,
		stored.ReportsDeprecated,
		stored.SkippedOnNoEmit,
	)
	if stored.Source != "" || stored.MessageText != "" {
		diagnostic.SetExternalData(stored.Source, stored.MessageText)
	}
	return diagnostic
}
