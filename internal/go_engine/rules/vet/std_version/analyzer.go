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
// same packages. The two agree everywhere, so there is no differences fixture.
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
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "stdversion",
	Doc: "report uses of too-new standard library symbols\n\n" +
		"The stdversion analyzer reports references to symbols in the standard\n" +
		"library that were introduced by a Go release higher than the one in\n" +
		"force in the referring file. (Recall that the file's Go version is\n" +
		"defined by the 'go' directive its module's go.mod file, or by a\n" +
		"\"//go:build go1.X\" build tag at the top of the file.)\n\n" +
		"The analyzer does not report a diagnostic for a reference to a \"too\n" +
		"new\" field or method of a type that is itself \"too new\", as this may\n" +
		"have false positives, for example if fields or methods are accessed\n" +
		"through a type alias that is guarded by a Go version constraint.\n",
	URL:              "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/stdversion",
	Requires:         []*analysis.Analyzer{inspect.Analyzer},
	RunDespiteErrors: true,
	Run:              run,
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

func run(pass *analysis.Pass) (any, error) {
	// Before go1.22 the type checker records no file versions, only the toolchain's, so the original turns itself
	// off. cohere builds with the toolchain go.mod pins, past go1.22, so this cannot return here; it is kept for
	// fidelity.
	if !slices.Contains(build.Default.ReleaseTags, "go1.22") {
		return nil, nil
	}
	// A module before go1.21 is not judged: its go directive was not yet a toolchain requirement. A package with no
	// version is newer than any, and is judged.
	packageVersion := pass.Pkg.GoVersion()
	if !atLeast(packageVersion, "go1.21") {
		return nil, nil
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("std_version: inspect's result is missing")
	}
	// Each package's too-new symbols at each version, worked out once.
	tooNew := map[tooNewKey]map[types.Object]symbol{}
	// fileVersion is the language version of the file being walked, and empty in a file that is not judged.
	fileVersion := ""
	// PreorderSeq visits each file and then its identifiers, as the original's Preorder does, so every identifier is
	// judged at its own file's version.
	for node := range inspected.PreorderSeq((*ast.File)(nil), (*ast.Ident)(nil)) {
		switch visited := node.(type) {
		case *ast.File:
			// A generated file, cgo's among them, is not judged. Otherwise the version may be empty, when the type
			// checker recorded none, and then the file is not judged either.
			fileVersion = ""
			if !ast.IsGenerated(visited) {
				fileVersion = version.Lang(recordedFileVersion(pass.TypesInfo, visited))
			}
		case *ast.Ident:
			if fileVersion == "" {
				continue
			}
			used, isUsed := pass.TypesInfo.Uses[visited]
			if !isUsed || used.Pkg() == nil {
				continue
			}
			key := tooNewKey{typesPackage: used.Pkg(), fileVersion: fileVersion}
			disallowed, computed := tooNew[key]
			if !computed {
				disallowed = tooNewSymbols(used.Pkg(), fileVersion)
				tooNew[key] = disallowed
			}
			standard, isTooNew := disallowed[origin(used)]
			if !isTooNew {
				continue
			}
			// The version is the module's unless the file's own build line set another, or the module's names a
			// patch release, whose language version the file's is.
			versionSource := "module"
			if fileVersion != packageVersion {
				versionSource = "file"
			}
			pass.ReportRangef(visited, "%s.%s requires %v or later (%s is %s)", used.Pkg().Name(), standard.name,
				minorVersion(standard.minor), versionSource, fileVersion)
		}
	}
	return nil, nil
}

// tooNewSymbols is the objects of the exported symbols of a standard-library package that are not yet available at
// fileVersion, each with its symbol. A package outside the standard library has none. typesPackage may hold type
// errors. x/tools' typesinternal.TooNewStdSymbols.
func tooNewSymbols(typesPackage *types.Package, fileVersion string) map[types.Object]symbol {
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
		if object := typesPackage.Scope().Lookup(standard.name); object != nil {
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
			typeName := typesPackage.Scope().Lookup(strings.TrimPrefix(receiver, "*"))
			if _, typeIsTooNew := disallowed[typeName]; typeName != nil && !typeIsTooNew {
				object, indices, _ = types.LookupFieldOrMethod(typeName.Type(), pointer, typesPackage, methodName)
			}
		} else if typeNameText, fieldName, isField := strings.Cut(standard.name, "."); isField {
			typeName := typesPackage.Scope().Lookup(typeNameText)
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
