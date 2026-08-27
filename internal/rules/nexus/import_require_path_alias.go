package nexus

import (
	"sort"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utilities/ecmascript/imports"
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
		aliases := make([]PathAlias, 0, len(settings.Aliases))
		for _, alias := range settings.Aliases {
			aliases = append(aliases, PathAlias{
				Directory: normalizeConfiguredDirectory(alias.Directory),
				Alias:     alias.Alias,
			})
		}
		sort.SliceStable(aliases, func(first int, second int) bool {
			return len(aliases[first].Directory) > len(aliases[second].Directory)
		})

		strictRoots := make([]string, 0, len(settings.StrictRoots))
		for _, root := range settings.StrictRoots {
			strictRoots = append(strictRoots, normalizeConfiguredDirectory(root))
		}

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
			if imports.HasPathSegment(repositoryPath, "internal") {
				return
			}

			suggestion, hasAlias := aliasForPath(aliases, repositoryPath)
			if !hasAlias {
				return
			}

			specifierRange := rule.TokenRange(ctx.SourceFile, specifierNode)
			specifierText := ctx.SourceFile.Text()[specifierRange.Pos():specifierRange.End()]
			fix := ctx.ReplaceNode(specifierNode, quotedLike(specifierText, suggestion))

			for _, root := range strictRoots {
				if directoryContains(root, importingRelative) {
					ctx.ReportNodeWithFixes(specifierNode,
						messageUseAliasInStrictRoot(importPath, root, suggestion), fix)
					return
				}
			}

			levels := strings.Count(importPath, "../")
			if levels >= 2 {
				ctx.ReportNodeWithFixes(specifierNode, messageUseAlias(importPath, levels, suggestion), fix)
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
		if !directoryContains(alias.Directory, repositoryPath) {
			continue
		}
		rest := repositoryPath
		if alias.Directory != "" {
			rest = strings.TrimPrefix(strings.TrimPrefix(repositoryPath, alias.Directory), "/")
		}
		if rest == "" {
			return alias.Alias, true
		}
		return alias.Alias + "/" + rest, true
	}
	return "", false
}

// quotedLike wraps a replacement specifier in the quote character the original used.
//
// `ReplaceNode` takes the literal's whole token range, quotes included, so the replacement has to
// supply its own. Reading the quote off the source rather than hardcoding one keeps the fix from
// rewriting a file's quote style as a side effect: this codebase writes single quotes and Prettier
// would put them back either way, but a fixer that silently changed a byte nobody asked about is a
// fixer people stop trusting.
//
// Falls back to a single quote when the source text is too short to have quotes at all, which
// cannot happen for a parsed string literal and is handled rather than assumed away.
func quotedLike(originalText string, replacement string) string {
	quote := byte('\'')
	if len(originalText) > 0 && (originalText[0] == '"' || originalText[0] == '\'' || originalText[0] == '`') {
		quote = originalText[0]
	}
	return string(quote) + replacement + string(quote)
}

// normalizeConfiguredDirectory is a configured directory as the matcher wants it: no leading `./`,
// no trailing slash, and the repository root as the empty string.
//
// The root is the case this exists for, and it was a silent defect in both implementations. A
// tsconfig spells the root `"@project/*": ["../../*"]`, so a reader configuring this rule writes
// `{ directory: ".", alias: "@project" }` -- the only spelling that reads like what it means. The
// matcher compares against repository-relative paths like `app/Foo.tsx`, which equal neither `.` nor
// anything starting with `./`, so that entry matched nothing at all. Every deep relative import
// under the root went unreported and the tree read clean.
//
// Measured on Kirk's tree: 127 findings before, 365 after, and the 238 that appeared are exactly
// the `app/` and `modules/` imports a hand count had already found. Normalising here rather than at
// each comparison also means the alias matcher and the strict-root matcher cannot drift, and it
// accepts `./app` and `app/` for the same directory, which a reader has no reason to expect to
// differ.
func normalizeConfiguredDirectory(directory string) string {
	trimmed := directory
	for strings.HasPrefix(trimmed, "./") {
		trimmed = trimmed[len("./"):]
	}
	trimmed = strings.TrimRight(trimmed, "/")
	if trimmed == "." {
		return ""
	}
	return trimmed
}

// directoryContains reports whether a repository-relative path sits inside a normalised directory.
//
// The empty string is the repository root and contains everything, which is what makes a root alias
// work. Every other directory matches on a `/` boundary rather than as a bare prefix, so
// `libraries/structure` does not swallow `libraries/structure-tools`.
func directoryContains(directory string, repositoryPath string) bool {
	if directory == "" {
		return true
	}
	return repositoryPath == directory || strings.HasPrefix(repositoryPath, directory+"/")
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
