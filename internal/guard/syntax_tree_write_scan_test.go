package guard

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"

	"github.com/system-inc/go-types/types"
)

// syntaxTreeWriteScan reads packages for writes into the syntax tree, and keeps what it found.
type syntaxTreeWriteScan struct {
	// root is the module's directory, which every site is named relative to.
	root string

	files     int
	treeFiles int

	// writes is every site found, `path:line what`, sorted.
	writes []string
}

// read scans one package's files, with their types, adding what it finds to the scan.
func (scan *syntaxTreeWriteScan) read(fileSet *token.FileSet, info *types.Info, files []*ast.File) {
	reader := &syntaxTreeWriteReader{scan: scan, fileSet: fileSet, info: info}
	reader.freshness(files)
	for _, file := range files {
		scan.files++
		for _, imported := range file.Imports {
			if path, unquoteError := strconv.Unquote(imported.Path.Value); unquoteError == nil &&
				syntaxTreePackages[path] {
				scan.treeFiles++
				break
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			reader.inspect(node)
			return true
		})
	}
	sort.Strings(scan.writes)
}
