// Package correctness_require_declared_walk is the Go engine's rule that a directory walk reaching outside its
// package declares what it reads.
//
// A test planner that keys each package's tests by the package's Go closure, its own testdata, and the reads lines
// kept in one file reruns a test only when something in that key changes. A test that walks a directory outside its
// package, with no reads line, isn't rerun when what it walks changes, so a breaking change lands on a stale green and
// the red surfaces later on someone else's change. This is a port of Adamic's repowalk, written after every such red
// in eight days turned out to be a walk: one test walked another package's _test.go files, and another walked the
// whole repository.
//
// A walk is filepath.WalkDir, filepath.Walk, fs.WalkDir, os.ReadDir, filepath.Glob, os.DirFS or parser.ParseDir. Its
// root leaves the package when it is a constant path that cleans to one starting with "..", what filepath.Join, Abs,
// Clean or Dir make of one, or the directory found by walking up from os.Getwd through filepath.Dir to the one that
// holds go.mod. The root is followed within the package through variables, parameters and results, since the real
// cases go through helpers: a test hands "../.." to a checker, which hands it to a lister, which walks it. While the
// path is known it is read against the package's own directory, so "../../internal/oracle/testdata" walked from
// internal/oracle stays in the package; joined with something the rule can't read, the path is judged by what is
// known of it so far. A root the rule can't trace, such as test.TempDir() or a directory another package computes,
// isn't flagged: the rule never guesses. A root that leaves the package only to open one fixed file is no walk, so it
// isn't flagged.
//
// The finding sits on the source, where the reach outward happens, since the walk itself may be in a helper many
// callers share: on the "../.." argument, or on the call to the function that walks up to the repository root. A
// `// Reads: <glob>` comment on that line or just above it declares the walk, one comment per glob, written as in the
// reads line. The rule doesn't hold the glob against the reads file; the planner does that from the comments.
//
// The rule is off until it is told which file holds the reads lines, through its readsFile option, since a
// repository with no such planner has nothing a declaration would feed.
package correctness_require_declared_walk

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-declared-walk"

// Analyzer runs the rule over one package.
var Analyzer = newAnalyzer()

// readsFileOption names the file of reads lines a planner holds each // Reads: glob against, such as Adamic's
// cloud/fast-gate/executors.txt, which Adamic's rule hardcoded. It is the rule's switch as well as its message's
// pointer: a repository with no such planner leaves it empty, and the rule reports nothing.
const readsFileOption = "readsFile"

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{
	Name:      Name,
	Origin:    rule.OriginHouse,
	TypeAware: true,
	Analyzer:  Analyzer,
	// ConfiguredBy lets the engine list the rule as not configured rather than quiet, since it reports nothing
	// until readsFile names the reads file.
	ConfiguredBy: []string{readsFileOption},
}

var undeclaredWalkMessage = policy.MessageOf(Name, "undeclaredWalk")

var readsWithoutGlobMessage = policy.MessageOf(Name, "readsWithoutGlob")

// readsMarker opens the comment that declares a walk. It is matched as written, with nothing before it, because a
// planner reads the glob after it, and a comment the rule took as a declaration the planner skipped would leave the
// walk undeclared with no finding to say so.
const readsMarker = "// Reads:"

// relatedWalkText labels the walk a finding's source reaches, for a reader that shows related positions.
const relatedWalkText = "the walk it reaches"

// deepestPath is how many elements a known path may grow to before the rest of it counts as unknown.
const deepestPath = 32

// walkSinks holds each walk by package path and function name, with the index of the argument it walks. fs.WalkDir
// walks its file system, whose root os.DirFS carries; its own root argument is a path inside that file system.
var walkSinks = map[string]map[string]int{
	"path/filepath": {"WalkDir": 0, "Walk": 0, "Glob": 0},
	"io/fs":         {"WalkDir": 0},
	"os":            {"ReadDir": 0, "DirFS": 0},
	"go/parser":     {"ParseDir": 1},
}

// pathCarriers are the path/filepath functions whose first result names the same place as their first argument, so a
// root that leaves the package still leaves it after them. filepath.Dir moves the place up, and is read apart.
var pathCarriers = map[string]bool{
	"Abs": true, "Clean": true, "EvalSymlinks": true, "FromSlash": true, "ToSlash": true,
}

func newAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "correctness_require_declared_walk",
		Doc: "Flags the source of a directory walk rooted outside the package's own directory, a constant path " +
			"that climbs out or a walk up to the directory holding go.mod, when no // Reads: <glob> comment on its " +
			"line or just above it declares what it reads. Reports nothing until the readsFile option names the " +
			"file of reads lines the planner checks those globs against.",
		Run: run,
	}
	analyzer.Flags.String(readsFileOption, "", "the file of reads lines a planner holds each // Reads: glob "+
		"against, named in the finding; empty turns the rule off")
	return analyzer
}

// readsFileOf is the readsFile option of the analyzer running pass. It is read from the pass rather than bound to a
// package variable, so a configured copy of the analyzer and the unconfigured original can run side by side.
func readsFileOf(pass *analysis.Pass) string {
	option := pass.Analyzer.Flags.Lookup(readsFileOption)
	if option == nil {
		return ""
	}
	return option.Value.String()
}

// origin is where a followed path comes from. A source that leaves the package has the position its // Reads:
// comment answers; a parameter is resolved to the arguments at each call. A root found by walking up to go.mod is
// moved, at each call of the function that walks up (walksUpIn), to that call, since the call is where a reader
// sees the reach outward.
//
// path is what is known of the path: for a source, the path as the program builds it, relative to the package's
// directory, and for a parameter, what is joined onto it. opaque says something the rule can't read was joined on,
// so path is only the part before it.
type origin struct {
	position  token.Pos
	parameter *types.Var
	walksUpIn *types.Func
	path      string
	opaque    bool
}

// assigned is one value given to a variable: an expression, or one result of a call that returns several.
type assigned struct {
	expression ast.Expr
	result     int
}

// function is what the rule knows of one function declared in the package.
type function struct {
	object      *types.Func
	declaration *ast.FuncDecl
	signature   *types.Signature
	// returns are the function's own return statements, a function literal's left out, and calls are every call in
	// its body, a function literal's included, since a closure walking what its function holds is the same walk.
	returns []*ast.ReturnStmt
	calls   []*ast.CallExpr
	// results are the origins of each result, and walked the origins that reach a walk, each with the walk's position.
	results []origins
	walked  map[origin]token.Pos
}

func run(pass *analysis.Pass) (any, error) {
	readsFile := readsFileOf(pass)
	if readsFile == "" || len(pass.Files) == 0 {
		return nil, nil
	}
	walker := &walker{
		pass: pass,
		// The file's own name, not one a //line directive gives it, since the walk runs from where the file sits.
		directory:   filepath.Dir(pass.Fset.PositionFor(pass.Files[0].Package, false).Filename),
		functions:   map[*types.Func]*function{},
		assignments: map[*types.Var][]assigned{},
		variables:   map[*types.Var]origins{},
		owners:      map[*types.Var]*function{},
		unreadable:  map[*types.Var]bool{},
		constants:   map[*types.Var][]string{},
	}
	walker.collect()
	// Every set only grows, and every origin is a position in the package or one of its parameters, with a path
	// joined from the package's own constants, so this ends.
	for walker.settle() {
	}
	walker.report(readsFile)
	return nil, nil
}

// earlier says found comes before position, where no position yet comes after everything.
func earlier(found token.Pos, position token.Pos) bool {
	return position == token.NoPos || found < position
}

// isPackageFunction says callee is a function, not a method, of the package at packagePath.
func isPackageFunction(callee *types.Func, packagePath string) bool {
	if callee.Pkg() == nil || callee.Pkg().Path() != packagePath {
		return false
	}
	return callee.Signature().Recv() == nil
}

// readsLines are the lines a // Reads: <glob> comment declares: its own, for a comment at the end of a line, and the
// line just after its comment group, so a walk reading several globs carries a comment for each above the source.
// globless are the lines a marker with nothing but spaces after it would have declared, so its finding can say the
// glob is missing; a line another comment declares is declared all the same.
func readsLines(fileSet *token.FileSet, file *ast.File) (declared map[int]bool, globless map[int]bool) {
	declared = map[int]bool{}
	globless = map[int]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			rest, isDeclaration := strings.CutPrefix(comment.Text, readsMarker)
			if !isDeclaration {
				continue
			}
			covered := []int{
				fileSet.PositionFor(comment.Pos(), false).Line,
				fileSet.PositionFor(group.End(), false).Line + 1,
			}
			glob, spaced := strings.CutPrefix(rest, " ")
			// An empty glob names nothing for the planner to check, and a comment holds one glob, written as in
			// the reads line, so anything else declares nothing. Only the empty one gets its own finding: a glob
			// written some other way is still a glob the planner won't read, which the plain finding covers.
			if !spaced || glob == "" || strings.ContainsFunc(glob, unicode.IsSpace) {
				if strings.TrimSpace(rest) == "" {
					for _, line := range covered {
						globless[line] = true
					}
				}
				continue
			}
			for _, line := range covered {
				declared[line] = true
			}
		}
	}
	return declared, globless
}
