package nexus

import (
	"sort"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utils/ecmascript/imports"
)

// PathAlias maps a repository-relative directory to the alias that names it.
type PathAlias struct {
	Directory string
	Alias     string
}

// ImportRequirePathAliasOptions configures which directories map to which alias.
type ImportRequirePathAliasOptions struct {
	// Aliases are matched longest directory first, so a nested root is not shadowed by its parent.
	Aliases []PathAlias

	// StrictRoots are directories where every relative import is reported, not only the deep ones.
	StrictRoots []string

	// RepositoryRoot is the absolute path the aliases are relative to.
	//
	// The TypeScript original reads process.cwd() here. That makes the rule's verdict depend on
	// where the linter was invoked from, which is the same latent defect boundary-no-internal-import
	// carried: correct from the repository root, silently inert anywhere else. Passing the root
	// explicitly makes the dependency visible and testable.
	RepositoryRoot string
}

func messageUseAlias(importPath string, levels int, suggestion string) rule.Message {
	return rule.Message{
		Id: "useAlias",
		Description: "'" + importPath + "' climbs " + strconv.Itoa(levels) + " levels, which says how far " +
			"up to walk rather than where it lands. A reader has to count directories to learn what it " +
			"means. Import it as '" + suggestion + "', which reads the same from any file and survives " +
			"this one moving.",
	}
}

func messageUseAliasInStrictRoot(importPath string, root string, suggestion string) rule.Message {
	return rule.Message{
		Id: "useAliasInStrictRoot",
		Description: "'" + importPath + "' is relative, and everything under '" + root + "' is imported " +
			"by alias so one path means one thing in every project that reads it. Import it as '" +
			suggestion + "'.",
	}
}

// ImportRequirePathAlias requires an alias for imports that leave their own neighbourhood.
//
//	valid:   import { Sibling } from './Sibling'
//	valid:   import { Parent } from '../Parent'
//	valid:   import { Thing } from '@base/source/foundation/Thing'
//	invalid: import { Thing } from '../../../foundation/Thing'
//
// A relative import that climbs two or more levels says how far up to walk rather than where it
// lands, so it stops carrying its own provenance. One level stays relative on purpose: `./Sibling`
// and `../Parent` are facts a reader already has from the file's own location, and spelling them as
// a full alias makes them longer without making them clearer.
//
// A strict root is a directory where even a sibling import must be aliased. A shared library read by
// several projects benefits from one spelling everywhere, so a path copied out of it lands correctly
// wherever it is pasted.
//
// A path holding an `internal` segment is exempt, and the reason is worth keeping: boundary-no-
// internal-import rejects an aliased path into an internal directory outright, because it wants a
// relative path to prove the importer is a neighbour. Reporting here would ask for the thing that
// rule rejects, and two rules that demand opposite edits are worse than either alone.
//
// No fix. The alias is derivable, but rewriting an import is only safe when the alias actually
// resolves in that project's config, which this rule cannot see.
var ImportRequirePathAlias = rule.Rule{
	Name: "import-require-path-alias",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		settings, hasSettings := options.(ImportRequirePathAliasOptions)
		if !hasSettings || len(settings.Aliases) == 0 || settings.RepositoryRoot == "" {
			// Without aliases there is nothing to suggest, and without a root nothing can be made
			// repository-relative. Declining is the honest answer, and it keeps a misconfigured
			// rule from guessing.
			return nil
		}

		// Longest directory first, so a nested root is not shadowed by its parent.
		aliases := append([]PathAlias(nil), settings.Aliases...)
		sort.SliceStable(aliases, func(first int, second int) bool {
			return len(aliases[first].Directory) > len(aliases[second].Directory)
		})

		repositoryRoot := strings.TrimSuffix(normalizedPathText(settings.RepositoryRoot), "/")
		importingFile := imports.NormalizedFileName(ctx.SourceFile)
		importingRelative, isInsideRepository := repositoryRelative(repositoryRoot, importingFile)
		if !isInsideRepository {
			return nil
		}

		check := func(node *ast.Node, specifierNode *ast.Node, importPath string) {
			if !strings.HasPrefix(importPath, ".") {
				return
			}

			resolved := tspath.NormalizePath(
				tspath.ResolvePath(tspath.GetDirectoryPath(importingFile), importPath))
			repositoryPath, isInside := repositoryRelative(repositoryRoot, resolved)
			// A path climbing out of the repository is not ours to name.
			if !isInside {
				return
			}
			// An internal import must stay relative; see the rule comment above.
			if hasPathSegment(repositoryPath, "internal") {
				return
			}

			suggestion, hasAlias := aliasForPath(aliases, repositoryPath)
			if !hasAlias {
				return
			}

			for _, root := range settings.StrictRoots {
				if importingRelative == root || strings.HasPrefix(importingRelative, root+"/") {
					ctx.ReportNode(specifierNode, messageUseAliasInStrictRoot(importPath, root, suggestion))
					return
				}
			}

			levels := strings.Count(importPath, "../")
			if levels >= 2 {
				ctx.ReportNode(specifierNode, messageUseAlias(importPath, levels, suggestion))
			}
		}

		return rule.Listeners{
			ast.KindImportDeclaration: func(node *ast.Node) {
				declaration := node.AsImportDeclaration()
				if declaration == nil || declaration.ModuleSpecifier == nil {
					return
				}
				if !ast.IsStringLiteralLike(declaration.ModuleSpecifier) {
					return
				}
				check(node, declaration.ModuleSpecifier, declaration.ModuleSpecifier.Text())
			},

			ast.KindCallExpression: func(node *ast.Node) {
				source, isImport := imports.CallExpressionSource(node)
				if !isImport {
					return
				}
				call := node.AsCallExpression()
				if call == nil || call.Arguments == nil || len(call.Arguments.Nodes) == 0 {
					return
				}
				check(node, call.Arguments.Nodes[0], source)
			},
		}
	},
}

// aliasForPath returns the aliased spelling of a repository-relative path.
func aliasForPath(aliases []PathAlias, repositoryPath string) (string, bool) {
	for _, alias := range aliases {
		if repositoryPath == alias.Directory {
			return alias.Alias, true
		}
		if strings.HasPrefix(repositoryPath, alias.Directory+"/") {
			return alias.Alias + "/" + strings.TrimPrefix(repositoryPath, alias.Directory+"/"), true
		}
	}
	return "", false
}

// repositoryRelative expresses an absolute path relative to the repository root, reporting whether
// it is inside it at all.
func repositoryRelative(repositoryRoot string, path string) (string, bool) {
	if path == repositoryRoot {
		return "", true
	}
	if !strings.HasPrefix(path, repositoryRoot+"/") {
		return "", false
	}
	return strings.TrimPrefix(path, repositoryRoot+"/"), true
}

// normalizedPathText rewrites Windows separators in a plain string.
func normalizedPathText(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
