package marker

import (
	"fmt"
	"go/ast"
	"reflect"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
)

// Analyzer indexes the comment groups of every file in a package once, for the rules that read a marker: each of them
// requires it and asks its file's index of the result, rather than building one of its own for every file it judges.
// It is a prerequisite and never a rule, so it reports nothing and the registry does not list it.
var Analyzer = &analysis.Analyzer{
	Name: "marker",
	Doc: "Indexes the comment groups of each file in a package by line, once, for the house rules that read a " +
		"reasoned marker comment. It reports nothing.",
	Requires:   []*analysis.Analyzer{positions.Analyzer},
	Run:        run,
	ResultType: reflect.TypeFor[*Indexes](),
	// It reads only comments and the line table, never type information, so a package with errors indexes as any
	// other. That keeps the syntax dispatcher, which requires what its listeners require, running on such a package
	// as it promises, so each rule there is skipped for the errors, never failed for a prerequisite.
	RunDespiteErrors: true,
}

// Indexes is what Analyzer gives: a package's comment indexes, one for each of its files.
type Indexes struct {
	table  *positions.Table
	byFile map[*ast.File]*Index
}

// ForFile is file's index. Every rule that requires Analyzer judges the files of the same pass, so each file it asks
// for was indexed; a file the package does not hold is indexed on the spot, the same index any other caller of
// NewIndex gets, rather than handed none.
func (indexes *Indexes) ForFile(file *ast.File) *Index {
	if index, indexed := indexes.byFile[file]; indexed {
		return index
	}
	return NewIndex(indexes.table, file)
}

func run(pass *analysis.Pass) (any, error) {
	table, isTable := pass.ResultOf[positions.Analyzer].(*positions.Table)
	if !isTable {
		return nil, fmt.Errorf("reading the line table: positions gave %T", pass.ResultOf[positions.Analyzer])
	}
	indexes := &Indexes{table: table, byFile: make(map[*ast.File]*Index, len(pass.Files))}
	// Every file, test files included, since some of the rules that read it judge only tests and others never do.
	for _, file := range pass.Files {
		indexes.byFile[file] = NewIndex(table, file)
	}
	return indexes, nil
}
