package dispatch

import (
	"go/ast"
	"go/token"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
)

// standingDeclarations is what the walk over the representation reads of a package's standing declarations
// (#e4rd5dw), decided once for every run as the walk starts and only read after, by every shard's walk at once.
type standingDeclarations struct {
	// files is the file of each standing declaration of a file some started run is confined on, by the position of
	// the opening brace of the declaration's body, which is one declaration's in the pass's whole file set. Every run
	// confined on a file holds the same spans for it, the package's, so they are read off the first.
	files map[token.Pos]*token.File
	// unheard is each of those files that every started run is confined on. A function standing there is heard by no
	// run, so the dispatcher builds the package without it.
	unheard map[*token.File]bool
}

// standingDeclarationsOf is the standing declarations of the files the runs of whole and parts are confined on, or
// nil when none is confined on any, at the cost of reading the length of whole's standing takes: whole holds the take
// of every started rule, whichever of them holds its run.
func standingDeclarationsOf(whole *startedRuns, parts []*startedRuns) *standingDeclarations {
	if len(whole.standing) == 0 {
		return nil
	}
	standing := &standingDeclarations{files: map[token.Pos]*token.File{}, unheard: map[*token.File]bool{}}
	started := 0
	confined := map[*token.File]int{}
	for _, each := range append([]*startedRuns{whole}, parts...) {
		for _, run := range each.runs {
			started++
			for tokenFile, spans := range run.standing {
				confined[tokenFile]++
				if confined[tokenFile] > 1 {
					continue
				}
				for _, span := range spans {
					standing.files[span.Body] = tokenFile
				}
			}
		}
	}
	for tokenFile, runs := range confined {
		if runs == started {
			standing.unheard[tokenFile] = true
		}
	}
	return standing
}

// fileOf is the file function stands in, for the runs confined on that file, or nil when it stands for no run. A
// declared function stands when its declaration is one of standing's, found by its body's opening brace, and a
// function literal exactly when the declared function enclosing it does, however deep it lies. The synthetic package
// initializer has no declaration, so it never stands, and neither does a function literal in a package-level
// initializer, whose enclosing function is that initializer: both are the build's own wherever anything stands
// (rule.StandingBodies).
func (standing *standingDeclarations) fileOf(function *intermediate_representation.Function) *token.File {
	enclosing := function
	for enclosing.Parent() != nil {
		enclosing = enclosing.Parent()
	}
	declaration, isDeclared := enclosing.Syntax().(*ast.FuncDecl)
	if !isDeclared || declaration.Body == nil {
		return nil
	}
	return standing.files[declaration.Body.Lbrace]
}

// isUnheard is whether declared, a declared function of the package, stands for every started run, when no listener
// is called for it or for a function literal inside it, and the dispatcher leaves it unbuilt.
func (standing *standingDeclarations) isUnheard(declared *intermediate_representation.Function) bool {
	tokenFile := standing.fileOf(declared)
	return tokenFile != nil && standing.unheard[tokenFile]
}
