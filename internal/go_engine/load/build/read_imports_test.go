package build

import (
	"bytes"
	"go/token"
	"slices"
	"testing"
)

// readGoInfo makes a file's imports at the length the parse counted and fills them by appending, so they end at that
// length, every one read from an import spec, in the order written, and a file of no imports has none. Grouped, lone,
// named and blank imports across three declarations each count once.
func TestAFilesImportsAreMadeAtTheCountTheParseRead(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		name    string
		source  string
		imports []string
	}{
		{
			name: "imports in three declarations",
			source: "package counted\n\nimport (\n\t\"fmt\"\n\tsorting \"sort\"\n)\n\nimport _ \"embed\"\n\n" +
				"import (\n\t. \"strings\"\n\t\"os\"\n)\n\nvar _, _, _, _ = fmt.Sprint, sorting.Ints, Cut, os.Exit\n",
			imports: []string{"fmt", "sort", "embed", "strings", "os"},
		},
		{name: "no imports", source: "package counted\n\nvar _ = 1\n"},
	} {
		read := fileInfo{name: "counted.go", fset: token.NewFileSet()}
		if readError := readGoInfo(bytes.NewReader([]byte(each.source)), &read); readError != nil {
			test.Fatalf("%s: reading failed: %v", each.name, readError)
		}
		var paths []string
		for _, imported := range read.imports {
			paths = append(paths, imported.path)
		}
		if !slices.Equal(paths, each.imports) {
			test.Errorf("%s: the imports read %q, want %q", each.name, paths, each.imports)
		}
		// Past the length, room an append never took; short of it, an append that regrew the slice.
		if cap(read.imports) != len(each.imports) {
			test.Errorf("%s: the imports are made with room for %d, want the %d the parse counted", each.name,
				cap(read.imports), len(each.imports))
		}
	}
}
