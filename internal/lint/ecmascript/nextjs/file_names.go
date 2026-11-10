package nextjs

import (
	"strings"
)

// conventionFileBaseNames are the base names Next.js reads from the filesystem by name.
//
// This is the framework's grammar, not an allowance. Next resolves a route segment by finding a file
// literally called `page` or `layout`, so the file's name is fixed before anyone writes the component
// in it, and there is no rename that would satisfy a naming rule without breaking the route. The
// component name in these files is free to carry what the path cannot, and it does: measured on the
// ahra tree on 2026-10-01, 77 of 80 `page.tsx` files default-export a `<Route>PageRoute` and every
// layout a `<Segment>Layout`. That is a naming convention of its own, and enforcing it would be a
// separate rule rather than a reading of structure/consistency-require-matching-file-name, which
// leaves these files alone, as nexus's file-name rules do.
//
// The list is Next 16.3.1's own, read from the installed build rather than from memory:
// `FILE_TYPES` and `HTTP_ACCESS_FALLBACKS` in `next/dist/build/webpack/loaders/next-app-loader`
// (layout, template, error, loading, global-error, global-not-found, not-found, forbidden,
// unauthorized), plus page, route and default, plus `mdx-components`, which Next resolves at the
// project root. The metadata image conventions are below, because they take a numeric suffix.
//
// It is wider than `FileContext.IsSpecialNextJsFile`, which names six of these plus route. Widening
// that one would move every rule reading it, `react-component-require-named-export` among them, and
// whether `template.tsx` should be exempt from those is their question rather than this list's.
//
// The base name alone decides, as it does in `FileContextFor`, so a `page.tsx` outside an app
// directory is exempt too. Measured, there is none in the ahra tree, and such a file's name is
// lowercase, which no component name can match anyway.
var conventionFileBaseNames = map[string]bool{
	"page":             true,
	"layout":           true,
	"template":         true,
	"loading":          true,
	"error":            true,
	"global-error":     true,
	"not-found":        true,
	"global-not-found": true,
	"forbidden":        true,
	"unauthorized":     true,
	"default":          true,
	"route":            true,
	"mdx-components":   true,

	// The metadata routes that are plain functions rather than images. They are `.ts` in practice
	// and listed for the `.tsx` spelling Next also accepts.
	"sitemap":  true,
	"robots":   true,
	"manifest": true,
}

// metadataImageBaseNames are the generated-image conventions, which Next also matches with a
// single trailing digit: `icon1.tsx` and `opengraph-image2.tsx` are routes. The pattern is
// `<name>\d?` in `next/dist/lib/metadata/is-metadata-route.js`.
var metadataImageBaseNames = []string{"icon", "apple-icon", "opengraph-image", "twitter-image"}

// writtenFileNames are the files Next.js writes into the project itself, under the name it chooses: `next-env.d.ts`,
// which `next dev` and `next build` rewrite at the project root on every run, so a rename is undone by the next one.
var writtenFileNames = map[string]bool{
	"next-env.d.ts": true,
}

// IsConventionFile reports a base name, the file's name without its folder or its extension, that Next.js reads by
// name.
func IsConventionFile(fileBaseName string) bool {
	if conventionFileBaseNames[fileBaseName] {
		return true
	}
	for _, imageBaseName := range metadataImageBaseNames {
		if fileBaseName == imageBaseName {
			return true
		}
		// One digit and only one, matching Next's `\d?`. `icon12.tsx` is not a route.
		if len(fileBaseName) == len(imageBaseName)+1 && strings.HasPrefix(fileBaseName, imageBaseName) {
			last := fileBaseName[len(fileBaseName)-1]
			if last >= '0' && last <= '9' {
				return true
			}
		}
	}
	return false
}

// IsWrittenByNext reports a file name, without its folder, that Next.js writes into the project under a name of its
// own choosing.
func IsWrittenByNext(fileName string) bool {
	return writtenFileNames[fileName]
}
