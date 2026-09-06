package structure

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/utilities/ecmascript/imports"
)

var messageNoDirectTanStackQuery = rule.Message{
	Id: "noDirectTanStackQuery",
	Description: "This imports @tanstack/react-query directly. NetworkService wraps it, and the " +
		"wrapper is where the query client, the cache keys, the retry policy and the error " +
		"shaping live. A component holding its own useQuery gets a different client and a " +
		"different cache, so its data goes stale independently of everything else on the page. " +
		"NetworkService.ts and Providers.tsx are exempt, since those are where TanStack is " +
		"configured.",
}

var messageNoDirectApollo = rule.Message{
	Id: "noDirectApollo",
	Description: "This imports Apollo directly. NetworkService replaced it rather than wrapping " +
		"it, so an Apollo import pulls a second GraphQL client into the bundle and gives this one " +
		"call site a cache nothing else reads or invalidates.",
}

var messageNoDirectGraphqlImport = rule.Message{
	Id: "noDirectGraphqlImport",
	Description: "This imports `graphql` from a generated path. Import `gql` from NetworkService " +
		"instead, which is the spelling that carries type information downstream. The generated " +
		"function produces a document the tooling cannot follow back to its query, so the types " +
		"at the call site degrade to unknown without anything failing.",
}

// NetworkNoForbiddenImport flags a direct import of a library NetworkService wraps or replaces.
//
//	valid:   import { useGraphQlQuery } from '@structure/source/services/network/NetworkService'
//	valid:   import { QueryClient } from '@tanstack/react-query'   (in Providers.tsx only)
//	invalid: import { useQuery } from '@tanstack/react-query'
//	invalid: import { gql } from '@apollo/client'
//	invalid: import { graphql } from '../generated/graphql'
//
// Three message ids because the three imports are forbidden for three different reasons, and a
// reader who sees the wrong one goes looking for the wrong fix. TanStack is wrapped, Apollo is
// replaced, and the generated `graphql` is a type-information problem rather than a client problem.
//
// The three tests are deliberately different shapes, matching the original. TanStack is an exact
// match with two exempt files, Apollo is a prefix match so every package under the scope is caught,
// and the generated import only reports when the `graphql` specifier is actually named, since a
// generated path is a normal thing to import types from.
//
// No fix. Each of the three wants a different replacement, and choosing it means knowing what the
// call site was doing with the import.
var NetworkNoForbiddenImport = rule.Rule{
	Name: "structure/network-no-forbidden-import",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		// TanStack is allowed where it is configured. Both files are named rather than pattern
		// matched, since the exemption is a fixed pair of places rather than a judgment.
		fileContext := FileContextFor(ctx.SourceFile.FileName())
		isTanStackExempt := fileContext.IsNetworkServiceFile ||
			strings.Contains(strings.ReplaceAll(ctx.SourceFile.FileName(), `\`, "/"), "Providers.tsx")

		return rule.Listeners{
			ast.KindImportDeclaration: func(node *ast.Node) {
				declaration := node.AsImportDeclaration()
				if declaration.ModuleSpecifier == nil || !ast.IsStringLiteralLike(declaration.ModuleSpecifier) {
					return
				}
				source := declaration.ModuleSpecifier.Text()

				if source == "@tanstack/react-query" && !isTanStackExempt {
					ctx.ReportNode(node, messageNoDirectTanStackQuery)
				}

				// A prefix match rather than an exact one, so every package in the scope is
				// caught rather than only the client entry point.
				if strings.HasPrefix(source, "@apollo/") {
					ctx.ReportNode(node, messageNoDirectApollo)
				}

				// Only when the graphql specifier is actually named. A generated path is an
				// ordinary place to import types from, so the path alone is not the defect.
				if strings.Contains(source, "/generated") && importsGraphqlSpecifier(declaration) {
					ctx.ReportNode(node, messageNoDirectGraphqlImport)
				}
			},
		}
	},
}

// importsGraphqlSpecifier reports whether a named import brings in `graphql`.
//
// The imported name is what counts rather than the local alias, so `import { graphql as query }`
// still reports: the alias changes what the call site says and not what was imported.
func importsGraphqlSpecifier(declaration *ast.ImportDeclaration) bool {
	if declaration.ImportClause == nil {
		return false
	}
	// `import { graphql as query }` carries the original name and the alias in two places, which
	// `ImportedNameOf` reads; asking for the local name would miss the aliased form.
	for _, element := range imports.BindingsOf(declaration.AsNode()).Named {
		if imports.ImportedNameOf(element) == "graphql" {
			return true
		}
	}
	return false
}
