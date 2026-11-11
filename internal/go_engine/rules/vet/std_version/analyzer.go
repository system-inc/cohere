// Package std_version is vet's stdversion pass, a reference to a standard-library symbol introduced by a Go release
// newer than the Go version in force in the referring file: its module's go directive, or the file's own
// `//go:build go1.N` line. A module before go1.21 is not judged, since the go directive was not yet a toolchain
// requirement, nor is a generated file. A too-new field or method of a type that is itself too new is not reported,
// since a type alias guarded by a build constraint may stand for an older shim with the same fields and methods.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/stdversion/stdversion.go, with
// the judgment of internal/typesinternal/toonew.go's TooNewStdSymbols it calls, whose logic it keeps and whose
// message it reports word for word, at the same position. The original reads x/tools' internal/stdlib manifest for
// each symbol's version, which cohere cannot import, so the port reads its own table, symbols_generated.go, which
// generate_symbols writes from the toolchain's `$GOROOT/api` files as x/tools' generator writes the manifest, and
// which TestTheCommittedTableIsTheToolchainsTable keeps current. The table holds every symbol the manifest gives a
// version after go1.0, the same 6,784 entries in the same order, and nothing else, since the pass never reports a
// symbol of go1.0. x/tools' internal/versions helpers are the public go/version they copy. The registry runs the port
// in the original's place, and the original stays the oracle: the parity tests beside this file run both over the
// same packages. The two agree everywhere, so there is no differences fixture. It sits on the engine's syntax
// dispatcher, hearing each file and each identifier on the walk the dispatcher makes once for every syntax rule.
package std_version

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/types"
	"go/version"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore` naming
// it read the same as before the port took the original's place.
const passName = "stdversion"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "report uses of too-new standard library symbols\n\n" +
	"The stdversion analyzer reports references to symbols in the standard\n" +
	"library that were introduced by a Go release higher than the one in\n" +
	"force in the referring file. (Recall that the file's Go version is\n" +
	"defined by the 'go' directive its module's go.mod file, or by a\n" +
	"\"//go:build go1.X\" build tag at the top of the file.)\n\n" +
	"The analyzer does not report a diagnostic for a reference to a \"too\n" +
	"new\" field or method of a type that is itself \"too new\", as this may\n" +
	"have false positives, for example if fields or methods are accessed\n" +
	"through a type alias that is guarded by a Go version constraint.\n"

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/stdversion"

// Listener is what the port hears of each package from the dispatcher: each file and then its identifiers, in
// preorder, as the original's Preorder visits them, so every identifier is judged at its own file's version.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.File)(nil), (*ast.Ident)(nil)},
	Node:  checkNode,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset, and runs it despite errors in the package, as the original runs.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	analyzer.RunDespiteErrors = true
	return analyzer
}

// symbol is one exported standard-library symbol, by the name x/tools' internal/stdlib manifest gives it, with the
// minor version of Go that introduced it. Its kind is in its name: a method's, concrete or an interface's, is
// `(*T).m` or `(T).m`, a field's is `T.f`, and a package-level function's, variable's, constant's or type's is bare.
type symbol struct {
	name  string
	minor int
}

// tooNewKey is one package judged at one file version, the key under which its too-new symbols are kept.
type tooNewKey struct {
	typesPackage *types.Package
	fileVersion  string
}

// packageState is what the port keeps over one package as the walk goes: whether the package is judged, read once, on
// the first node heard, since the listener has no hook as the walk enters the package, the version of the file being
// walked, and the too-new symbols worked out so far.
type packageState struct {
	// isRead is whether isJudged and packageVersion have been read.
	isRead bool
	// isJudged is whether the package is judged at all: the toolchain records file versions, and the module is at
	// go1.21 or later.
	isJudged bool
	// packageVersion is the package's Go version, empty when it has none.
	packageVersion string
	// fileVersion is the language version of the file being walked, and empty in a file that is not judged.
	fileVersion string
	// tooNew is each package's too-new symbols at each version, worked out once.
	tooNew map[tooNewKey]map[types.Object]symbol
}

// checkNode reads each file's version as the walk enters it, and reports each identifier in a judged file that uses a
// standard-library symbol newer than that version.
func checkNode(context *rule.Context, node ast.Node, _ []ast.Node) {
	state := rule.State[packageState](context)
	if !state.isRead {
		state.isRead = true
		state.packageVersion = context.Package.GoVersion()
		// Before go1.22 the type checker records no file versions, only the toolchain's, so the original turns itself
		// off. cohere builds with the toolchain go.mod pins, past go1.22, so this cannot leave the package unjudged;
		// it is kept for fidelity. A module before go1.21 is not judged: its go directive was not yet a toolchain
		// requirement. A package with no version is newer than any, and is judged.
		state.isJudged = slices.Contains(build.Default.ReleaseTags, "go1.22") &&
			atLeast(state.packageVersion, "go1.21")
		state.tooNew = map[tooNewKey]map[types.Object]symbol{}
	}
	if !state.isJudged {
		return
	}
	switch visited := node.(type) {
	case *ast.File:
		// A generated file, cgo's among them, is not judged. Otherwise the version may be empty, when the type
		// checker recorded none, and then the file is not judged either.
		state.fileVersion = ""
		if !ast.IsGenerated(visited) {
			state.fileVersion = version.Lang(recordedFileVersion(context.TypesInfo, visited))
		}
	case *ast.Ident:
		if state.fileVersion == "" {
			return
		}
		used, isUsed := context.TypesInfo.Uses[visited]
		if !isUsed || used.Pkg() == nil {
			return
		}
		key := tooNewKey{typesPackage: used.Pkg(), fileVersion: state.fileVersion}
		disallowed, computed := state.tooNew[key]
		if !computed {
			disallowed = tooNewSymbols(context, used.Pkg(), state.fileVersion)
			state.tooNew[key] = disallowed
		}
		standard, isTooNew := disallowed[origin(used)]
		if !isTooNew {
			return
		}
		// The version is the module's unless the file's own build line set another, or the module's names a patch
		// release, whose language version the file's is.
		versionSource := "module"
		if state.fileVersion != state.packageVersion {
			versionSource = "file"
		}
		context.ReportRangef(visited, "%s.%s requires %v or later (%s is %s)", used.Pkg().Name(), standard.name,
			minorVersion(standard.minor), versionSource, state.fileVersion)
	}
}

// tooNewSymbols is the objects of the exported symbols of a standard-library package that are not yet available at
// fileVersion, each with its symbol. A package outside the standard library has none. typesPackage may hold type
// errors. x/tools' typesinternal.TooNewStdSymbols. Each look into typesPackage's scope is recorded in the context's
// reads, an answer that is a type holding its fields and methods in its own rendering (#b2t9xmf).
func tooNewSymbols(context *rule.Context, typesPackage *types.Package, fileVersion string) map[types.Object]symbol {
	disallowed := map[types.Object]symbol{}
	// These packages' symbols are available before their release under build tags this pass cannot see, so they are
	// not judged there: testing/synctest from go1.24 with goexperiment.synctest, and json v2 from go1.25 with
	// goexperiment.jsonv2.
	if typesPackage.Path() == "testing/synctest" && atLeast(fileVersion, "go1.24") {
		return disallowed
	}
	if (typesPackage.Path() == "encoding/json/v2" || typesPackage.Path() == "encoding/json/jsontext") &&
		atLeast(fileVersion, "go1.25") {
		return disallowed
	}

	// First the package-level symbols.
	symbols := standardSymbols[typesPackage.Path()]
	for _, standard := range symbols {
		if !before(fileVersion, minorVersion(standard.minor)) || strings.Contains(standard.name, ".") {
			continue
		}
		// A symbol this build of the package lacks, one for another platform, has no object, and no identifier can
		// use it. The original keeps it under a nil key, which no use ever looks up.
		if object := context.LookupIn(typesPackage, standard.name); object != nil {
			disallowed[object] = standard
		}
	}

	// Then the fields and methods, but only of a type that is not itself too new: a compatibility shim declares the
	// type twice under build tags, the new API's for new versions and a fake's for old ones, and a reference through
	// the shim compiles at the old version with the fake's fields and methods, whichever declaration the analysis
	// happens to see. Where embedding makes two symbols name the same object, the one with the shorter path of
	// embedded fields wins, and of two at the same depth, the first.
	depth := map[types.Object]int{}
	for _, standard := range symbols {
		if !before(fileVersion, minorVersion(standard.minor)) {
			continue
		}
		var object types.Object
		var indices []int
		if receiver, methodName, isMethod := splitMethod(standard.name); isMethod {
			pointer := strings.HasPrefix(receiver, "*")
			typeName := context.LookupIn(typesPackage, strings.TrimPrefix(receiver, "*"))
			if _, typeIsTooNew := disallowed[typeName]; typeName != nil && !typeIsTooNew {
				object, indices, _ = types.LookupFieldOrMethod(typeName.Type(), pointer, typesPackage, methodName)
			}
		} else if typeNameText, fieldName, isField := strings.Cut(standard.name, "."); isField {
			typeName := context.LookupIn(typesPackage, typeNameText)
			if _, typeIsTooNew := disallowed[typeName]; typeName != nil && !typeIsTooNew {
				object, indices, _ = types.LookupFieldOrMethod(typeName.Type(), false, typesPackage, fieldName)
			}
		}
		if object == nil {
			continue
		}
		if shallowest, seen := depth[object]; !seen || len(indices) < shallowest {
			depth[object] = len(indices)
			disallowed[object] = standard
		}
	}
	return disallowed
}

// splitMethod is a method symbol's receiver, with its `*` when it has one, and its method's name: `(*Buffer).Grow`
// is `*Buffer` and `Grow`. It is false for a symbol that is not a method. x/tools' stdlib.Symbol.SplitMethod.
func splitMethod(name string) (receiver string, methodName string, isMethod bool) {
	parenthesized, found := strings.CutPrefix(name, "(")
	if !found {
		return "", "", false
	}
	receiver, methodName, _ = strings.Cut(parenthesized, ").")
	return receiver, methodName, true
}

// origin is the declaration an object instantiates, the object itself when it instantiates nothing: a generic type's,
// function's or field's own, so a use of `List[int]` is judged as one of `List`. An alias is left as written, since
// the alias, not what it stands for, is the symbol named. stdversion.go's origin.
func origin(object types.Object) types.Object {
	switch declared := object.(type) {
	case *types.Var:
		return declared.Origin()
	case *types.Func:
		return declared.Origin()
	case *types.TypeName:
		if named, isNamed := declared.Type().(*types.Named); isNamed {
			return named.Origin().Obj()
		}
	}
	return object
}

// recordedFileVersion is the Go version the type checker recorded for a file, from its build line or its package's,
// and empty when it recorded none that is valid. x/tools' versions.FileVersion.
func recordedFileVersion(information *types.Info, file *ast.File) string {
	if recorded := information.FileVersions[file]; version.IsValid(recorded) {
		return recorded
	}
	return ""
}

// minorVersion is the version string of Go 1's minor release: 22 is `go1.22`. x/tools' stdlib.Version.String.
func minorVersion(minor int) string {
	return fmt.Sprintf("go1.%d", minor)
}

// atLeast reports whether a version is release or later; an empty one is an unknown future version, later than any.
// x/tools' versions.AtLeast.
func atLeast(goVersion string, release string) bool {
	if goVersion == "" {
		return true
	}
	return version.Compare(version.Lang(goVersion), version.Lang(release)) >= 0
}

// before reports whether a version is earlier than release; an empty one is an unknown future version, earlier than
// none. x/tools' versions.Before.
func before(goVersion string, release string) bool {
	if goVersion == "" {
		return false
	}
	return version.Compare(version.Lang(goVersion), version.Lang(release)) < 0
}
