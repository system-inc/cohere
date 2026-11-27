package parity

import (
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"

	"golang.org/x/tools/go/types/objectpath"
)

// ownFactAnswers are what the own-facts differential read over one package, each answer keyed by its kind and its
// object's key (ownFactKey): the original's answer and cohere's, the object a key names for a failure, and by kind how
// many answers were compared and on how many an unexported function's the original holds the fact.
type ownFactAnswers struct {
	original map[string]string
	ours     map[string]string
	names    map[string]string
	compared map[string]int
	held     map[string]int

	// marked is cohere's odd slice answer about each parameter the probe asked, compared after the run with the
	// original's marks, which only the original's own facts hold.
	marked map[*types.Var]bool
}

// newOwnFactAnswers is a package's answers with none read yet.
func newOwnFactAnswers() ownFactAnswers {
	return ownFactAnswers{original: map[string]string{}, ours: map[string]string{}, names: map[string]string{},
		compared: map[string]int{}, held: map[string]int{}, marked: map[*types.Var]bool{}}
}

// recordOwnFact keeps in answers the original's answer and cohere's about object, function's own or function itself,
// under kind, suffix telling apart a function's several answers of one kind. holds is whether the original holds the
// fact there, counted when function is unexported, since those are the facts no importer inherits.
//
// Mutates: answers its maps gain the answer and its counts
func recordOwnFact(answers ownFactAnswers, fileSet *token.FileSet, kind string, object types.Object,
	function *types.Func, suffix string, original string, ours string, holds bool) {
	key := kind + " " + ownFactKey(fileSet, object) + suffix
	answers.original[key], answers.ours[key] = original, ours
	answers.names[key] = kind + " of " + function.FullName() + suffix
	answers.compared[kind]++
	if holds && !function.Exported() {
		answers.held[kind]++
	}
}

// ownFactKey is object's key among its package's answers: its objectpath, or, for an object with none, as an
// unexported function or its parameter, its position in the load, the same for both sides.
func ownFactKey(fileSet *token.FileSet, object types.Object) string {
	path, pathError := objectpath.For(object)
	if pathError == nil {
		return "objectpath " + string(path)
	}
	position := fileSet.Position(object.Pos())
	// The module is one directory, so its file's name and the line and column tell every object apart.
	return fmt.Sprintf("declared at %s:%d:%d", filepath.Base(position.Filename), position.Line, position.Column)
}
