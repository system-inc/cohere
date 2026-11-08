// Package positions is the line table rules read a node's file, line and column from, built once per package from
// each file's own line offsets and read without a lock. A rule that asks the pass's FileSet instead takes the set's
// lock and its atomics for every node, on every core at once: in a cold run on cohere's own root that was 5.8s of the
// house rules' 19.6s of CPU, most of it inside marker.NewIndex, which seven rules build for every file.
//
// A rule requires Analyzer and reads its Result, a *Table, from pass.ResultOf, so every rule on a package shares the
// one table the package's files were read into.
package positions

import (
	"cmp"
	"go/ast"
	"go/token"
	"reflect"
	"slices"
	"sort"

	"golang.org/x/tools/go/analysis"
)

// Analyzer builds the package's Table. Its Result is a *Table.
var Analyzer = &analysis.Analyzer{
	Name:       "positions",
	Doc:        "the line table of every file the package's rules are given, read without the FileSet's lock",
	Run:        run,
	ResultType: reflect.TypeFor[*Table](),
	// It reads only the file set's line tables, never type information, so a package with errors gives its table as
	// any other, for marker and the syntax dispatcher, which run despite errors too.
	RunDespiteErrors: true,
}

// Table answers what fileSet.PositionFor(position, false) answers for any position in the files it was built from,
// as a file holds it and never as a `//line` directive names it, since a rule judges the file as written. A position
// in no file it holds, as an imported object's, is read from the FileSet it was built over.
type Table struct {
	fileSet *token.FileSet

	// files are the files the table holds, by base, ascending.
	files []fileLines
}

// fileLines is one file's span in the FileSet and the offset each of its lines starts at, the first at 0.
type fileLines struct {
	name        string
	base        int
	size        int
	lineOffsets []int
}

func run(pass *analysis.Pass) (any, error) {
	return New(pass.Fset, pass.Files), nil
}

// New is the table of syntax's files, as fileSet holds them. A file fileSet does not hold is left out, and its
// positions are read from fileSet.
func New(fileSet *token.FileSet, syntax []*ast.File) *Table {
	table := &Table{fileSet: fileSet, files: make([]fileLines, 0, len(syntax))}
	for _, each := range syntax {
		tokenFile := fileSet.File(each.FileStart)
		if tokenFile == nil {
			continue
		}
		table.files = append(table.files, fileLines{
			name:        tokenFile.Name(),
			base:        tokenFile.Base(),
			size:        tokenFile.Size(),
			lineOffsets: tokenFile.Lines(),
		})
	}
	slices.SortFunc(table.files, func(left, right fileLines) int { return cmp.Compare(left.base, right.base) })
	return table
}

// Position is position's file, offset, line and column, as fileSet.PositionFor(position, false) gives them, or the
// zero Position for token.NoPos.
func (table *Table) Position(position token.Pos) token.Position {
	if !position.IsValid() {
		return token.Position{}
	}
	at := int(position)
	// The last file whose base is at or before the position; a file's span runs from its base to base+size, both
	// ends included, since its end of file is a position too.
	index := sort.Search(len(table.files), func(each int) bool { return table.files[each].base > at }) - 1
	if index < 0 || at > table.files[index].base+table.files[index].size {
		return table.fileSet.PositionFor(position, false)
	}
	held := table.files[index]
	offset := at - held.base
	// The line is the count of lines that start at or before offset, as token.File's own search finds it.
	line := sort.Search(len(held.lineOffsets), func(each int) bool { return held.lineOffsets[each] > offset })
	return token.Position{
		Filename: held.name,
		Offset:   offset,
		Line:     line,
		Column:   offset - held.lineOffsets[line-1] + 1,
	}
}

// Line is position's line in its file as written, or 0 for token.NoPos.
func (table *Table) Line(position token.Pos) int {
	return table.Position(position).Line
}
