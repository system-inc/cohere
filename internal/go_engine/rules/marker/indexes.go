package marker

import (
	"go/ast"
	"go/token"
	"reflect"

	"golang.org/x/tools/go/analysis"
)

// Analyzer indexes the comment groups of every file in a package once, for the rules that read a marker: each of them
// requires it and asks its file's index of the result, rather than building one of its own for every file it judges.
// It is a prerequisite and never a rule, so it reports nothing and the registry does not list it.
var Analyzer = &analysis.Analyzer{
	Name: "marker",
	Doc: "Indexes the comment groups of each file in a package by line, once, for the house rules that read a " +
		"reasoned marker comment. It reports nothing.",
	Run:        run,
	ResultType: reflect.TypeFor[*Indexes](),
}

// Indexes is what Analyzer gives: a package's comment indexes, one for each of its files.
type Indexes struct {
	fileSet *token.FileSet
	byFile  map[*ast.File]*Index
}

// ForFile is file's index. Every rule that requires Analyzer judges the files of the same pass, so each file it asks
// for was indexed; a file the package does not hold is indexed on the spot, the same index any other caller of
// NewIndex gets, rather than handed none.
func (indexes *Indexes) ForFile(file *ast.File) *Index {
	if index, indexed := indexes.byFile[file]; indexed {
		return index
	}
	return NewIndex(indexes.fileSet, file)
}

func run(pass *analysis.Pass) (any, error) {
	indexes := &Indexes{fileSet: pass.Fset, byFile: make(map[*ast.File]*Index, len(pass.Files))}
	// Every file, test files included, since some of the rules that read it judge only tests and others never do.
	for _, file := range pass.Files {
		indexes.byFile[file] = NewIndex(pass.Fset, file)
	}
	return indexes, nil
}
