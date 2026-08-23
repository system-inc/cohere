// Package format re-exports typescript-go's formatter.
//
// Hand-written rather than generated, for the same reason the parser shim is: gen_shims covers the
// packages a headless linter needs, and formatting is not among them.
//
// Note this is internal/format, the editor's formatter, and not internal/printer, which is the
// emitter. The emitter produces JavaScript from an AST and does not preserve comments or the
// author's line breaks. The formatter returns text changes against the original source, which is
// both what formatting means and the same shape our fixes already take.
package format

import (
	"context"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/format"
)

// FormatCodeSettings is what the formatter is configured with.
type FormatCodeSettings = format.FormatCodeSettings

// WithFormatCodeSettings puts settings on a context, which is how the formatter reads them.
func WithFormatCodeSettings(ctx context.Context, settings *FormatCodeSettings, newLine string) context.Context {
	return format.WithFormatCodeSettings(ctx, settings, newLine)
}

// GetDefaultFormatCodeSettings returns TypeScript's own defaults.
func GetDefaultFormatCodeSettings(newLine string) *FormatCodeSettings {
	return format.GetDefaultFormatCodeSettings(newLine)
}

// FormatDocument returns the text changes that would format a whole file.
func FormatDocument(ctx context.Context, sourceFile *ast.SourceFile) []core.TextChange {
	return format.FormatDocument(ctx, sourceFile)
}
