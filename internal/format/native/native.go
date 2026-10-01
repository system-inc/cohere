// Package native is cohere's own formatter: the printers that replace Prettier, routed by file type.
//
// It is the candidate the differential measures as "native", and the formatter the format phase
// switches to once every corpus reads 100%. Until a language's printer exists, files of that type are
// refused rather than passed through, so the harness reads 0% for them instead of crediting a printer
// that does not exist.
//
// # One file per printer, so two Circles never share one
//
// Printers register from their own file in this package, `typescript.go` and `markdown.go`, each
// calling Register in an init function. Two Circles port in parallel, and a shared routing table would
// hold both of their uncommitted edits at once, so a commit by pathspec would carry the other's work.
// A file each keeps every commit to its owner's lines.
package native

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/format/prettier"
)

// Print formats one file's text with the options its repository resolves to.
//
// fileName is passed because a printer may need it: Prettier picks json-stringify for package.json by
// name, not by extension.
type Print func(fileName string, text string, options prettier.Options) (string, error)

var (
	mutex    sync.RWMutex
	printers = map[string]Print{}
)

// Register routes an extension (".tsx", with the dot) to a printer. Registering one twice is a
// programming error, because two printers claiming a file type would make the result depend on init
// order, so it panics at startup rather than formatting with whichever won.
func Register(extension string, print Print) {
	mutex.Lock()
	defer mutex.Unlock()
	extension = strings.ToLower(extension)
	if _, taken := printers[extension]; taken {
		panic(fmt.Sprintf("native: %s is registered twice", extension))
	}
	printers[extension] = print
}

// Extensions lists what is registered, for a report to say which languages the native side covers.
func Extensions() []string {
	mutex.RLock()
	defer mutex.RUnlock()
	extensions := make([]string, 0, len(printers))
	for extension := range printers {
		extensions = append(extensions, extension)
	}
	sort.Strings(extensions)
	return extensions
}

// Formatter formats with the registered printers and one set of options.
type Formatter struct {
	Options prettier.Options
}

// Handles reports whether a printer is registered for the file's type.
func (formatter Formatter) Handles(fileName string) bool {
	_, present := lookup(fileName)
	return present
}

// Format formats a file, or refuses one no printer handles.
func (formatter Formatter) Format(fileName string, text string) (string, error) {
	print, present := lookup(fileName)
	if !present {
		return "", fmt.Errorf("native: no printer for %s yet", fileName)
	}
	return print(fileName, text, formatter.Options)
}

func lookup(fileName string) (Print, bool) {
	mutex.RLock()
	defer mutex.RUnlock()
	print, present := printers[strings.ToLower(filepath.Ext(fileName))]
	return print, present
}
