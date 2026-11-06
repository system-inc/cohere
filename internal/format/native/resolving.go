package native

import (
	"fmt"
	"path/filepath"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/gitignore"
)

// Resolving formats each file with the options its own directory resolves to, the way
// prettier.Resolving does for the goja engine, so switching the format phase from one to the other
// changes the printers and nothing about which options a file is printed with.
//
// It holds no engines, because a native printer costs nothing to construct: only the run's Resolver,
// which remembers each directory's options.
type Resolving struct {
	resolver *format_options.Resolver
}

// NewResolving resolves startDirectory's options eagerly, so a config cohere refuses stops the run
// before any phase does work rather than on the first file after fixes have begun.
func NewResolving(startDirectory string) (*Resolving, error) {
	resolving := &Resolving{resolver: format_options.NewResolver()}
	if _, resolveError := resolving.resolver.Resolve(startDirectory); resolveError != nil {
		return nil, resolveError
	}
	return resolving, nil
}

// Handles reports whether a native printer is registered for the file's type.
func (resolving *Resolving) Handles(fileName string) bool {
	_, present := lookup(fileName)
	return present
}

// Enumerate walks a tree for the files a native printer handles.
func (resolving *Resolving) Enumerate(root string) (format_files.Enumeration, error) {
	return format_files.Enumerate(root, resolving.Handles)
}

// EnumerateListed is Enumerate reading directories from listing where it holds them. See
// format_files.EnumerateListed.
func (resolving *Resolving) EnumerateListed(root string, listing gitignore.Listing) (format_files.Enumeration, error) {
	return format_files.EnumerateListed(root, resolving.Handles, listing)
}

// Format formats one file with the options its own directory resolves to.
func (resolving *Resolving) Format(fileName string, text string) (string, error) {
	return resolving.FormatParsed(fileName, text, nil)
}

// FormatParsed is Format with a tree of the text someone already parsed, or nil (Formatter.FormatParsed).
func (resolving *Resolving) FormatParsed(fileName string, text string, parsed *ast.SourceFile) (string, error) {
	resolution, resolveError := resolving.resolver.Resolve(filepath.Dir(fileName))
	if resolveError != nil {
		return "", fmt.Errorf("resolving the format options for %s: %w", fileName, resolveError)
	}
	return Formatter{Options: resolution.Options}.FormatParsed(fileName, text, parsed)
}

// OptionsFingerprint names the options a file formats with: every field, by name, so two resolutions
// that differ anywhere print differently.
func (resolving *Resolving) OptionsFingerprint(fileName string) (string, error) {
	resolution, resolveError := resolving.resolver.Resolve(filepath.Dir(fileName))
	if resolveError != nil {
		return "", fmt.Errorf("resolving the format options for %s: %w", fileName, resolveError)
	}
	return fmt.Sprintf("%+v", resolution.Options), nil
}
