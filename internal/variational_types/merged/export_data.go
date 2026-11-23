package merged

import (
	"fmt"
	"go/token"
	"io"
	"os"
	"time"

	"github.com/system-inc/go-types/gcimporter"
	"github.com/system-inc/go-types/types"
)

// A Source is one build's export data, read into that build's own universe of the fork's types. Each build's
// universe is its own: no type of one is a type of another, and the merge is what makes one universe of them all.
//
// A source's types may read export data when they are first used (a lazy scope entry, a named type's loader), so the
// importer reads a build's packages and every type in them from one goroutine at a time, holding that build's lock. A
// source needn't be safe for concurrent use.
type Source interface {
	// Import reads path's package into the build's universe, with whatever it needs of the packages path imports,
	// unless the universe holds it complete already, and is it. It is nil, with no error, when the build doesn't
	// compile path.
	Import(path string) (*types.Package, error)
	// Reached is path's package as the build's universe holds it so far, complete or only reached through another
	// package's export data, or nil when the universe holds none. A package only reached holds the objects the export
	// data that reached it refers to, so the importer reads path whole only for a name it lacks.
	Reached(path string) *types.Package
	// Elapsed is the time the source has spent reading, on Import and on every read on demand since.
	Elapsed() time.Duration
}

// ExportData is the source of one build that reads the gc export data files the build compiled, with the fork's
// gcimporter: files maps each import path the build compiles to its file. Each package's names are read when it is
// imported, and each object and each named type's declaration the first time it is needed.
func ExportData(fileSet *token.FileSet, files map[string]string) Source {
	return &exportData{fileSet: fileSet, files: files, universe: gcimporter.NewUniverse()}
}

// exportData is ExportData's source.
type exportData struct {
	fileSet  *token.FileSet
	files    map[string]string
	universe *gcimporter.Universe
	imported time.Duration // the time Import spent, beside the universe's own reads on demand
}

func (source *exportData) Import(path string) (*types.Package, error) {
	file, compiled := source.files[path]
	if !compiled {
		return nil, nil
	}
	if decoded := source.universe.Packages[path]; decoded != nil && decoded.Complete() {
		return decoded, nil
	}
	started := time.Now()
	defer func() { source.imported += time.Since(started) }()
	decoded, importError := gcimporter.Import(source.fileSet, source.universe, path, "",
		func(asked string) (io.ReadCloser, error) {
			askedFile, askedCompiled := source.files[asked]
			if !askedCompiled {
				return nil, fmt.Errorf("no export data for %q", asked)
			}
			return os.Open(askedFile)
		})
	if importError != nil {
		return nil, fmt.Errorf("%s: %w", file, importError)
	}
	return decoded, nil
}

func (source *exportData) Reached(path string) *types.Package { return source.universe.Packages[path] }

func (source *exportData) Elapsed() time.Duration { return source.imported + source.universe.Elapsed }
