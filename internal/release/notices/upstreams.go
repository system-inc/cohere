package notices

// Reach is where an upstream's work reaches a user.
type Reach string

const (
	// Compiled is code ported or vendored into cohere's source and compiled into the binary.
	Compiled Reach = "Compiled"

	// RepositoryOnly is kept in the repository for tests and tools, and ships in no package.
	RepositoryOnly Reach = "RepositoryOnly"
)

// Upstream is a project whose code or data cohere carries in its own source, which no module graph
// can find: a port, a vendored copy, or data captured from it.
type Upstream struct {
	Name    string
	URL     string
	License string

	// Version is the version ported, when the port names one. Empty for code ported from the project
	// over time rather than from one release.
	Version string

	// Carries says what in cohere holds the work.
	Carries string

	Reach Reach

	// LicenseFile is the license text under licenses/, taken at Version when there is one, since a
	// project can relicense later releases (postcss-values-parser moved from MIT to MPL-2.0 at 3.0).
	LicenseFile string
}

// Upstreams are the curated projects, in the order they are written: vendored code first, then the
// formatter's ports, then the linter's.
var Upstreams = []Upstream{
	{
		Name: "tsgolint", URL: "https://github.com/typescript-eslint/tsgolint", License: "MIT",
		Carries: "The shim generator behind TypeScript-shim/ and the checker helpers vendored in internal/lint/checking.",
		Reach:   Compiled, LicenseFile: "tsgolint.txt",
	},
	{
		Name: "rslint", URL: "https://github.com/web-infra-dev/rslint", License: "MIT",
		Carries: "The control-flow graph vendored in internal/lint/ecmascript/control_flow_graph.",
		Reach:   Compiled, LicenseFile: "rslint.txt",
	},
	{
		Name: "Prettier", URL: "https://github.com/prettier/prettier", License: "MIT",
		Carries: "The formatter in internal/format: its document model and printers are ported from Prettier's.",
		Reach:   Compiled, LicenseFile: "prettier.txt",
	},
	{
		Name: "yaml", URL: "https://github.com/eemeli/yaml", License: "ISC", Version: "2",
		Carries: "The YAML parser in internal/format/yaml, ported.",
		Reach:   Compiled, LicenseFile: "yaml.txt",
	},
	{
		Name: "yaml-unist-parser", URL: "https://github.com/prettier/yaml-unist-parser", License: "MIT", Version: "3",
		Carries: "The YAML syntax tree in internal/format/yaml/unist, ported.",
		Reach:   Compiled, LicenseFile: "yaml-unist-parser.txt",
	},
	{
		Name: "postcss-values-parser", URL: "https://github.com/shellscape/postcss-values-parser", License: "MIT", Version: "2.0.1",
		Carries: "The CSS value parser in internal/format/css/values, ported from 2.0.1, which was MIT.",
		Reach:   Compiled, LicenseFile: "postcss-values-parser.txt",
	},
	{
		Name: "micromark", URL: "https://github.com/micromark/micromark", License: "MIT",
		Carries: "The Markdown tokenizer in internal/format/markdown/micromark, ported.",
		Reach:   Compiled, LicenseFile: "micromark.txt",
	},
	{
		Name: "mdast-util-from-markdown", URL: "https://github.com/syntax-tree/mdast-util-from-markdown", License: "MIT",
		Carries: "The Markdown syntax tree builder in internal/format/markdown/mdast, ported.",
		Reach:   Compiled, LicenseFile: "mdast-util-from-markdown.txt",
	},
	{
		Name: "emoji-regex", URL: "https://github.com/mathiasbynens/emoji-regex", License: "MIT", Version: "10.6.0",
		Carries: "Emoji ranges captured into internal/format/doc/string_width_generated.go.",
		Reach:   Compiled, LicenseFile: "emoji-regex.txt",
	},
	{
		Name: "get-east-asian-width", URL: "https://github.com/sindresorhus/get-east-asian-width", License: "MIT", Version: "1.6.0",
		Carries: "Character widths captured into internal/format/doc/string_width_generated.go.",
		Reach:   Compiled, LicenseFile: "get-east-asian-width.txt",
	},
	{
		Name: "narrow-emojis", URL: "https://github.com/fisker/narrow-emojis", License: "MIT", Version: "0.0.3",
		Carries: "Narrow emoji ranges captured into internal/format/doc/string_width_generated.go.",
		Reach:   Compiled, LicenseFile: "narrow-emojis.txt",
	},
	{
		Name: "ESLint", URL: "https://github.com/eslint/eslint", License: "MIT",
		Carries: "Core lint rules in internal/lint/rules/core, ported.",
		Reach:   Compiled, LicenseFile: "eslint.txt",
	},
	{
		Name: "eslint-utils", URL: "https://github.com/eslint-community/eslint-utils", License: "MIT",
		Carries: "The reference tracker in internal/lint/ecmascript/reference, ported.",
		Reach:   Compiled, LicenseFile: "eslint-utils.txt",
	},
	{
		Name: "typescript-eslint", URL: "https://github.com/typescript-eslint/typescript-eslint", License: "MIT",
		Carries: "TypeScript lint rules in internal/lint/rules/typescript, ported.",
		Reach:   Compiled, LicenseFile: "typescript-eslint.txt",
	},
	{
		Name: "oxc", URL: "https://github.com/oxc-project/oxc", License: "MIT",
		Carries: "Lint rules ported from oxc's, across internal/lint/rules.",
		Reach:   Compiled, LicenseFile: "oxc.txt",
	},
	{
		Name: "eslint-plugin-react", URL: "https://github.com/jsx-eslint/eslint-plugin-react", License: "MIT",
		Carries: "React lint rules in internal/lint/rules/react, ported.",
		Reach:   Compiled, LicenseFile: "eslint-plugin-react.txt",
	},
	{
		Name: "React", URL: "https://github.com/facebook/react", License: "MIT",
		Carries: "eslint-plugin-react-hooks and the React Compiler's rules, in internal/lint/rules/react and internal/lint/ecmascript/high_level_intermediate_representation, ported.",
		Reach:   Compiled, LicenseFile: "react.txt",
	},
	{
		Name: "Next.js", URL: "https://github.com/vercel/next.js", License: "MIT",
		Carries: "@next/eslint-plugin-next's rules in internal/lint/rules/next, ported.",
		Reach:   Compiled, LicenseFile: "next.js.txt",
	},
	{
		Name: "Tailwind CSS", URL: "https://github.com/tailwindlabs/tailwindcss", License: "MIT",
		Carries: "Tailwind's class model in internal/lint/rules/tailwind, ported.",
		Reach:   Compiled, LicenseFile: "tailwindcss.txt",
	},
	{
		Name: "prettier-plugin-tailwindcss", URL: "https://github.com/tailwindlabs/prettier-plugin-tailwindcss", License: "MIT",
		Carries: "Tailwind class ordering in internal/lint/rules/tailwind, ported.",
		Reach:   Compiled, LicenseFile: "prettier-plugin-tailwindcss.txt",
	},
	{
		Name: "eslint-plugin-better-tailwindcss", URL: "https://github.com/schoero/eslint-plugin-better-tailwindcss", License: "MIT",
		Carries: "Tailwind lint rules in internal/lint/rules/tailwind, ported.",
		Reach:   Compiled, LicenseFile: "eslint-plugin-better-tailwindcss.txt",
	},
	{
		Name: "Prettier's bundled builds", URL: "https://github.com/system-inc/prettier", License: "MIT",
		Carries: "Built Prettier bundles vendored in internal/format/prettier/bundles, which tests compare the formatter against. They bundle many other projects, whose licenses are in internal/format/prettier/bundles/THIRD-PARTY-NOTICES.md.",
		Reach:   RepositoryOnly, LicenseFile: "prettier.txt",
	},
}

// moduleLicenses is each Go module's license, read from its license file, for every module in the build
// graph or go.mod. A module missing here stops the generator by name, so a new dependency is read
// before it is credited.
var moduleLicenses = map[string]string{
	"github.com/Microsoft/go-winio":     "MIT",
	"github.com/klauspost/cpuid/v2":     "MIT",
	"github.com/dlclark/regexp2/v2":     "MIT",
	"github.com/zeebo/xxh3":             "BSD-2-Clause",
	"golang.org/x/sync":                 "BSD-3-Clause",
	"golang.org/x/sys":                  "BSD-3-Clause",
	"golang.org/x/text":                 "BSD-3-Clause",
	"github.com/dop251/goja":            "MIT",
	"github.com/go-sourcemap/sourcemap": "BSD-2-Clause",
	"github.com/google/pprof":           "Apache-2.0",
	"golang.org/x/mod":                  "BSD-3-Clause",
	"golang.org/x/tools":                "BSD-3-Clause",
}

// swiftLicense is a Swift package's license expression and its text under licenses/.
type swiftLicense struct {
	Expression string
	File       string
}

// swiftLicenses are the Swift engine's packages, by their identity in swift/Package.resolved, each
// license taken at the pinned revision.
var swiftLicenses = map[string]swiftLicense{
	"swift-argument-parser": {Expression: "Apache-2.0 WITH Swift-exception", File: "swift-argument-parser.txt"},
	"swift-cmark":           {Expression: "BSD-2-Clause AND MIT", File: "swift-cmark.txt"},
	"swift-format":          {Expression: "Apache-2.0 WITH Swift-exception", File: "swift-format.txt"},
	"swift-markdown":        {Expression: "Apache-2.0 WITH Swift-exception", File: "swift-markdown.txt"},
	"swift-syntax":          {Expression: "Apache-2.0 WITH Swift-exception", File: "swift-syntax.txt"},
}
