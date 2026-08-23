package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/verify/internal/rule"
)

var (
	messageAliasedInternal = rule.Message{
		Id: "aliasedInternal",
		Description: "An aliased or absolute import must not reach through 'internal'. The folder is named " +
			"internal to say it belongs to exactly one owner, and an alias erases the distance that made " +
			"that ownership legible.",
	}
	messageOutsideInternal = rule.Message{
		Id: "outsideInternal",
		Description: "Only files inside the folder that owns this 'internal' directory may import from it. " +
			"An internal folder is the one place a module may keep something it is free to change without " +
			"warning, and that freedom lasts exactly as long as nobody outside reaches in.",
	}
)

// BoundaryNoInternalImport confines an internal folder to the folder that owns it.
//
//	valid:   /a/b/Thing.ts        imports './internal/Detail'
//	valid:   /a/b/c/Thing.ts      imports '../internal/Detail'
//	invalid: /a/b/Thing.ts        imports '@structure/x/internal/Detail'
//	invalid: /a/other/Thing.ts    imports '../b/internal/Detail'
//
// The owning folder is the directory directly above the last 'internal' segment in the resolved
// import path. Files at or below that directory may import from it; everything else may not.
//
// Two details are load-bearing. The last 'internal' is what counts, not the first: a path with
// nested internal folders is owned by the innermost one, which is the stricter and correct reading.
// And the comparison resolves the relative specifier against the importing file's own directory
// rather than against the process working directory, which is what the TypeScript original did.
// That original compared two paths both made relative to cwd, so it agreed with this one whenever
// the linter ran from the repository root and silently stopped enforcing anything when it did not.
// Resolving against the importing file has no such dependence.
var BoundaryNoInternalImport = rule.Rule{
	Name: "boundary-no-internal-import",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}
		importingFile := normalizedFileName(ctx.SourceFile)

		return importSourceVisitors(func(source string, node *ast.Node) {
			checkInternalImport(ctx, node, importingFile, source)
		})
	},
}

// checkInternalImport decides one specifier against one importing file.
func checkInternalImport(ctx rule.Context, node *ast.Node, importingFile string, source string) {
	if !strings.Contains(source, "internal") {
		return
	}

	// An alias or a bare package name gives no distance to reason about, so it cannot be shown to
	// be inside the owning folder and is refused outright. A relative specifier is the only shape
	// that can prove it belongs.
	if !strings.HasPrefix(source, ".") {
		if hasPathSegment(source, "internal") {
			ctx.ReportNode(importSpecifierNode(node), messageAliasedInternal)
		}
		return
	}

	resolved := tspath.NormalizePath(tspath.ResolvePath(tspath.GetDirectoryPath(importingFile), source))

	segments := strings.Split(resolved, "/")
	internalIndex := -1
	for index, segment := range segments {
		if segment == "internal" {
			internalIndex = index
		}
	}
	// Index zero would mean a path rooted at "internal" with no owner above it, which no real file
	// layout produces and which has no folder to compare against.
	if internalIndex <= 0 {
		return
	}

	owningFolder := strings.Join(segments[:internalIndex], "/")
	if !strings.HasPrefix(importingFile, owningFolder+"/") {
		ctx.ReportNode(importSpecifierNode(node), messageOutsideInternal)
	}
}

// hasPathSegment reports whether a specifier contains a path segment named exactly segment.
//
// Matching the bare substring would flag '@structure/internalization/Thing' for the letters it
// happens to contain, and a boundary rule that fires on an unrelated word teaches people to
// disable it.
func hasPathSegment(path string, segment string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == segment {
			return true
		}
	}
	return false
}
