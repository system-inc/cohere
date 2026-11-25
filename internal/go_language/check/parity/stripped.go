package parity

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check/checker"
)

// Strip is what StrippedFindings takes out of a fixture package's type check: the bodies of the functions it names,
// as stage 3's build checks a package whose standing bodies it strips (#9fbrnra). The type check is of shallow copies
// of the files, each stripped function's declaration copied without its body, so the copies share every other node,
// the signatures' Name, Recv and Type among them, and keep every import spec. The analyzer reads the original syntax,
// where a stripped function still has its body, now with no types recorded inside it.
//
// The two wrong strips are for a red, each a way the real strip could go wrong that a rule's answer would show.
type Strip struct {
	// Functions are the names of the top-level functions and methods whose bodies the type check does not see. Each
	// must name at least one with a body, since stripping nothing would prove nothing.
	Functions []string

	// BodilessSyntax is the wrong strip that hands the analyzer the stripped copies too, so a stripped function looks
	// declared without a body, as an assembly function's Go declaration is.
	BodilessSyntax bool

	// DroppingImports is the wrong strip whose copies also lose each import spec that only stripped bodies use, so the
	// package's imports and its files' scopes no longer hold it.
	DroppingImports bool
}

// StrippedFindings loads the fixture packages patterns name from testdata, runs analyzer over every root of each load
// twice, plain and with strip's functions stripped from its type check, and returns each run's findings, sorted, each
// file named as Findings names it, so a test compares the two finding for finding. Each root is type-checked against
// its imports as loaded, so an external test package sees the package it tests unstripped; a body never decides a
// package-level type, so the stripped types of an import would be the same. A type error the strip makes other than an
// import left unused fails the test, and so does a name of strip's that strips nothing. It loads for the machine's own
// target.
func StrippedFindings(
	test testing.TB,
	analyzer *analysis.Analyzer,
	testdata string,
	strip Strip,
	patterns ...string,
) (plainFindings []Finding, strippedFindings []Finding) {
	test.Helper()
	defer Target{}.held(test)()
	plainFindings, strippedFindings = []Finding{}, []Finding{}
	stripped := map[string]bool{}
	for _, fixtures := range loadFixtures(test, Target{}, testdata, patterns, nil) {
		strippedRoots := []*packages.Package{}
		for _, root := range fixtures.roots {
			strippedRoot, strippedNames := strippedPackage(test, root, strip)
			strippedRoots = append(strippedRoots, strippedRoot)
			for _, name := range strippedNames {
				stripped[name] = true
			}
		}
		plainFindings = append(plainFindings, findingsOf(test, analyzer, fixtures.roots, fixtures.fileName)...)
		strippedFindings = append(strippedFindings, findingsOf(test, analyzer, strippedRoots, fixtures.fileName)...)
	}
	for _, name := range strip.Functions {
		if !stripped[name] {
			test.Fatalf("the strip names %s, and no function of that name has a body, so it would prove nothing", name)
		}
	}
	slices.SortFunc(plainFindings, compareFindings)
	slices.SortFunc(strippedFindings, compareFindings)
	return plainFindings, strippedFindings
}

// Messages is each of findings' messages, in order, for a test that holds a stripped run to what a rule reports.
func Messages(findings []Finding) []string {
	messages := []string{}
	for _, finding := range findings {
		messages = append(messages, finding.Message)
	}
	return messages
}

// strippedPackage is root with its types and TypesInfo from a type check of shallow copies of its files, strip's
// functions copied without their bodies, and its syntax the original files unless the strip is BodilessSyntax, with the
// name of each function it stripped.
func strippedPackage(
	test testing.TB,
	root *packages.Package,
	strip Strip,
) (strippedRoot *packages.Package, strippedNames []string) {
	test.Helper()
	strippedNames = []string{}
	dropped := map[ast.Spec]bool{}
	if strip.DroppingImports {
		dropped = importsOnlyStrippedBodiesUse(root, strip.Functions)
	}
	copies := make([]*ast.File, 0, len(root.Syntax))
	for _, file := range root.Syntax {
		copied := *file
		copied.Decls = make([]ast.Decl, 0, len(file.Decls))
		copied.Imports = slices.DeleteFunc(slices.Clone(file.Imports), func(spec *ast.ImportSpec) bool {
			return dropped[spec]
		})
		for _, declaration := range file.Decls {
			switch declared := declaration.(type) {
			case *ast.FuncDecl:
				if declared.Body != nil && slices.Contains(strip.Functions, declared.Name.Name) {
					bodiless := *declared
					bodiless.Body = nil
					copied.Decls = append(copied.Decls, &bodiless)
					strippedNames = append(strippedNames, declared.Name.Name)
					continue
				}
			case *ast.GenDecl:
				if declared.Tok == token.IMPORT && slices.ContainsFunc(declared.Specs, func(spec ast.Spec) bool {
					return dropped[spec]
				}) {
					kept := *declared
					kept.Specs = slices.DeleteFunc(slices.Clone(declared.Specs), func(spec ast.Spec) bool {
						return dropped[spec]
					})
					copied.Decls = append(copied.Decls, &kept)
					continue
				}
			}
			copied.Decls = append(copied.Decls, declaration)
		}
		copies = append(copies, &copied)
	}

	// The configuration is the load's own (load/loader.go's check), but for its errors: an import that only stripped
	// bodies use is left unused, which the strip must allow, and anything else is the strip's mistake.
	information := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	checked := types.NewPackage(root.PkgPath, root.Name)
	configuration := &types.Config{
		Importer: importerFunction(func(path string) (*types.Package, error) {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			if imported := root.Imports[path]; imported != nil {
				return imported.Types, nil
			}
			return nil, fmt.Errorf("no metadata for %s", path)
		}),
		Error: func(found error) {
			if typeError, isTypeError := found.(types.Error); isTypeError &&
				strings.Contains(typeError.Msg, " imported") && strings.HasSuffix(typeError.Msg, " and not used") {
				return
			}
			test.Errorf("type-checking %s stripped of %v: %v", root.ID, strip.Functions, found)
		},
		Sizes:       root.TypesSizes,
		GoVersion:   root.Types.GoVersion(),
		FakeImportC: true,
	}
	// Ignored: every error already went to the configuration's Error function, which judged it.
	_ = types.NewChecker(configuration, root.Fset, checked, information).Files(copies)

	// A file's scope and version are recorded by its node, and a copy is a node of its own, so each is moved to the
	// original file the analyzer reads, as a strip must move them for a rule that reads a file's scope.
	syntax := root.Syntax
	if strip.BodilessSyntax {
		syntax = copies
	}
	for index, copied := range copies {
		information.Scopes[syntax[index]] = information.Scopes[copied]
		information.FileVersions[syntax[index]] = information.FileVersions[copied]
	}

	copiedRoot := *root
	copiedRoot.Types = checked
	copiedRoot.TypesInfo = information
	copiedRoot.Syntax = syntax
	return &copiedRoot, strippedNames
}

// importsOnlyStrippedBodiesUse is each import spec of root whose package name, by the plain type check's record, is
// used only inside the bodies of the functions named, and at least once there.
func importsOnlyStrippedBodiesUse(root *packages.Package, functions []string) map[ast.Spec]bool {
	insideStripped := func(position token.Pos) bool {
		for _, file := range root.Syntax {
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if isFunction && function.Body != nil && slices.Contains(functions, function.Name.Name) &&
					function.Body.Pos() <= position && position < function.Body.End() {
					return true
				}
			}
		}
		return false
	}
	usedInside, usedOutside := map[types.Object]bool{}, map[types.Object]bool{}
	for identifier, object := range root.TypesInfo.Uses {
		if _, isPackageName := object.(*types.PkgName); !isPackageName {
			continue
		}
		if insideStripped(identifier.Pos()) {
			usedInside[object] = true
		} else {
			usedOutside[object] = true
		}
	}
	dropped := map[ast.Spec]bool{}
	for _, file := range root.Syntax {
		for _, spec := range file.Imports {
			if name := root.TypesInfo.PkgNameOf(spec); name != nil && usedInside[name] && !usedOutside[name] {
				dropped[spec] = true
			}
		}
	}
	return dropped
}

// findingsOf is what analyzer found over roots, each file named by fileName, the run failing the test where the
// analyzer did not run, since silence there would read as agreement.
func findingsOf(
	test testing.TB,
	analyzer *analysis.Analyzer,
	roots []*packages.Package,
	fileName func(string) string,
) []Finding {
	test.Helper()
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{analyzer}, roots, engineOptions())
	if analyzeError != nil {
		test.Fatalf("analyzing %d packages: %v", len(roots), analyzeError)
	}
	found := []Finding{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s on %s: %v", analyzer.Name, action.Package.ID, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found = append(found, newFinding(action.Package.Fset, diagnostic).WithFileNames(fileName))
		}
	}
	return found
}
