package standing_presence

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/build_reads"
	"github.com/system-inc/cohere/internal/go_language/fact_reach"
)

// screamingNamed names, with screaming's reason, each object the package's bodies use that the build finds as a
// package-level constant of the pass's own package with an underscore in its name, declared in a file that imports a
// system package: the rule exempts such a constant where any comparison in the package ties it to the system constant
// of its name (markComparedConstants, rules/consistency_no_screaming_snake_case/analyzer.go:573-590), so each body
// that names it must be present.
//
// The predicate mirrors the rule's, which exposes none of it: a system package is syscall, golang.org/x/sys or one
// below it (isSystemPackage, analyzer.go:542-545), a file importing one is a system file (systemFilesOf,
// analyzer.go:555-566), and only a name with an underscore, never the blank one, holds a finding the exemption can
// lift (judge, analyzer.go:281-286). As the census counter reads it, a constant the rule drops for another reason, a
// directed name or one with no MixedCaps form, is a candidate all the same: wider is the safe side.
func screamingNamed(
	pass *analysis.Pass,
	used []fact_reach.UsedObject,
	find func(identity build_reads.Identity) (types.Object, bool),
) []fact_reach.Named {
	// candidates are read on the first constant of the package found, since most packages' bodies use none.
	var candidates map[*types.Const]bool
	var named []fact_reach.Named
	for _, object := range used {
		found, isFound := find(object.Identity)
		constant, isConstant := found.(*types.Const)
		if !isFound || !isConstant || constant.Pkg() != pass.Pkg || constant.Parent() != pass.Pkg.Scope() {
			continue
		}
		if candidates == nil {
			candidates = systemConstants(pass)
		}
		if candidates[constant] {
			named = append(named, fact_reach.Named{Object: object.Object, Reason: Screaming.Reason()})
		}
	}
	return named
}

// systemConstants are the package-level constants with an underscore in their names declared in the pass's system
// files.
func systemConstants(pass *analysis.Pass) map[*types.Const]bool {
	constants := map[*types.Const]bool{}
	for _, file := range pass.Files {
		if !importsSystemPackage(file) {
			continue
		}
		for _, declaration := range file.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.CONST {
				continue
			}
			for _, specification := range general.Specs {
				value, isValue := specification.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				for _, name := range value.Names {
					declared, isConstant := pass.TypesInfo.Defs[name].(*types.Const)
					if isConstant && name.Name != "_" && strings.Contains(name.Name, "_") {
						constants[declared] = true
					}
				}
			}
		}
	}
	return constants
}

// importsSystemPackage is whether file imports syscall, golang.org/x/sys or a package below it.
func importsSystemPackage(file *ast.File) bool {
	for _, specification := range file.Imports {
		path, unquoteError := strconv.Unquote(specification.Path.Value)
		if unquoteError == nil &&
			(path == "syscall" || path == "golang.org/x/sys" || strings.HasPrefix(path, "golang.org/x/sys/")) {
			return true
		}
	}
	return false
}
