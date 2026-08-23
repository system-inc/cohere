package structure

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

// messageForbiddenThemeValue names the value, the component, and what is allowed instead.
//
// The allowed list is the whole finding. "This value is not allowed" sends the author hunting for
// the theme file; naming the values it can be is the edit.
func messageForbiddenThemeValue(component string, propertyName string, value string, allowed []string) rule.Message {
	return rule.Message{
		Id: "forbiddenThemeValue",
		Description: "<" + component + " " + propertyName + "=\"" + value + "\"> uses a value the " +
			"library does not define. A shared component that renders a project's value has a " +
			"dependency pointing the wrong way: the library would need the project to build, and " +
			"the next project to use this component finds a value that means nothing to it. Use " +
			"one the library defines: " + strings.Join(allowed, ", ") + ". If the value belongs in " +
			"the library, add it to the theme first.",
	}
}

// themePropertySuffixes maps a JSX property name to the interface suffix that defines its values:
// a `variant` property is checked against `ButtonVariantsInterface`.
var themePropertySuffixes = map[string]string{
	"variant":  "Variants",
	"size":     "Sizes",
	"kind":     "Kinds",
	"position": "Positions",
	"side":     "Sides",
}

// componentAliases names components that render another component's theme.
//
// `AnimatedButton` has no `AnimatedButtonVariantsInterface`; it wraps `Button` and takes Button's
// values. Without this the rule finds no theme for it and declines, which is silent rather than
// wrong but leaves the component unguarded.
var componentAliases = map[string]string{
	"AnimatedButton": "Button",
	"TipButton":      "Button",
	"Tip":            "Popover",
}

// interfaceNameSuffix is the trailing word every theme interface in the tree carries.
const interfaceNameSuffix = "Interface"

// BoundaryNoProjectThemeValue reports a theme value used in library code that the library does not
// define.
//
//	valid:   <Button variant="Ghost" />        inside libraries/structure, and Ghost is in the theme
//	valid:   <Button variant="Emphasized" />   outside libraries/structure, where projects extend
//	invalid: <Button variant="Emphasized" />   inside libraries/structure
//
// Ported from `structure/boundary-no-project-theme-value`.
//
// # The cache is built from the type graph rather than from the filesystem
//
// This is a deliberate divergence from the original and the only one in this rule.
//
// The original discovers its theme files by walking `process.cwd() + "libraries/structure/source"`
// with `readdirSync`, parsing each match with a second `@typescript-eslint/parser` instance, and
// caching the result in a module-level variable. It does that because ESLint hands a rule one file
// at a time and gives it no way to see the others. Verify has the whole program, so this reads the
// same files out of `ctx.Program` instead.
//
// Two defects are avoided rather than ported, and both are ones this codebase has already paid for:
//
//   - `process.cwd()` makes the verdict depend on where the linter was invoked from. That is the
//     invisible dependency `import-require-path-alias` had, fixed there by taking the root as a
//     required option rather than reading it from the process.
//   - The original wraps its parse in `catch { return {} }`, so an unparseable theme file yields an
//     empty cache and the rule goes quiet. Quiet is indistinguishable from clean, which is the
//     failure this whole tool exists to remove.
//
// **The two mechanisms were measured against each other rather than assumed equivalent.** The
// filesystem walk finds 28 files; the program holds 29 whose names end in `Theme.ts`. The extra one
// is `source/theme/hooks/useTheme.ts`, which the original's `^[A-Z]\w*Theme\.ts$` pattern correctly
// rejects for its lowercase first letter. It exports one hook and no theme interface, so it
// contributes nothing either way and the two mechanisms see the same 28 contributing files. The
// pattern is reproduced here anyway, so the difference stays at zero rather than at "currently
// harmless".
//
// # Only inside the library
//
// A project using its own values is the system working. The boundary is one-directional: the
// library may not reach into the project, and the project may extend the library freely.
var BoundaryNoProjectThemeValue = rule.Rule{
	Name: "boundary-no-project-theme-value",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil || !FileContextFor(ctx.SourceFile.FileName()).IsInLibrariesStructure {
			return nil
		}
		if ctx.Program == nil {
			return nil
		}

		themes := themeValuesFromProgram(ctx)
		if len(themes) == 0 {
			// No themes found means the rule cannot decide anything, and reporting nothing would be
			// indistinguishable from a clean tree. Declining is the honest answer; the coverage
			// line's "listened to no files" note is what makes the silence visible.
			return nil
		}

		return rule.Listeners{
			ast.KindJsxAttribute: func(node *ast.Node) {
				attribute := node.AsJsxAttribute()
				name := attribute.Name()
				if name == nil || name.Kind != ast.KindIdentifier {
					return
				}
				suffix, isThemeProperty := themePropertySuffixes[name.Text()]
				if !isThemeProperty {
					return
				}

				// A string literal only. `variant={expression}` cannot be checked without evaluating
				// it, and guessing is how a boundary rule starts reporting on correct code.
				if attribute.Initializer == nil || attribute.Initializer.Kind != ast.KindStringLiteral {
					return
				}
				value := attribute.Initializer.Text()

				componentName := jsxElementNameOf(node)
				if componentName == "" {
					return
				}
				componentTheme, found := themes[componentName]
				if !found {
					if aliased, isAlias := componentAliases[componentName]; isAlias {
						componentTheme, found = themes[aliased]
					}
				}
				if !found {
					return
				}

				allowed := componentTheme[suffix]
				if len(allowed) == 0 {
					return
				}
				for _, allowedValue := range allowed {
					if allowedValue == value {
						return
					}
				}
				ctx.ReportNode(attribute.Initializer,
					messageForbiddenThemeValue(componentName, name.Text(), value, allowed))
			},
		}
	},
}

// jsxElementNameOf returns the component name of the element an attribute belongs to.
//
// Two element kinds carry attributes, and that is a real difference from the original rather than a
// detail. ESTree has one `JSXOpeningElement` for both forms; typescript-go distinguishes a
// self-closing element from an opening one. A port checking a single kind would silently skip every
// `<Button variant="X" />`, which is the dominant form in this codebase.
//
// A qualified name (`<Foo.Bar />`) returns empty. It is not a bare identifier, so it names no
// component in the theme cache, and the original's `JSXIdentifier` check excludes it the same way.
func jsxElementNameOf(attribute *ast.Node) string {
	attributes := attribute.Parent
	if attributes == nil || attributes.Kind != ast.KindJsxAttributes {
		return ""
	}
	element := attributes.Parent
	if element == nil {
		return ""
	}

	var tagName *ast.Node
	switch element.Kind {
	case ast.KindJsxSelfClosingElement:
		tagName = element.AsJsxSelfClosingElement().TagName
	case ast.KindJsxOpeningElement:
		tagName = element.AsJsxOpeningElement().TagName
	default:
		return ""
	}
	if tagName == nil || tagName.Kind != ast.KindIdentifier {
		return ""
	}
	return tagName.Text()
}

// themeValuesFromProgram collects every theme interface's keys, by component and by suffix.
//
// Cached per run on the file cache rather than in a package variable. The original memoizes in a
// module-level variable, which is correct for a process that lints once and exits and wrong for
// anything that reuses the process across trees.
func themeValuesFromProgram(ctx rule.Context) map[string]map[string][]string {
	themes := map[string]map[string][]string{}

	for _, sourceFile := range ctx.Program.SourceFiles() {
		if sourceFile == nil || !isThemeFileName(sourceFile.FileName()) {
			continue
		}
		sourceFile.AsNode().ForEachChild(func(statement *ast.Node) bool {
			if statement.Kind != ast.KindInterfaceDeclaration {
				return false
			}
			declaration := statement.AsInterfaceDeclaration()
			// Exported only, matching the original, which looks for an interface inside an
			// ExportNamedDeclaration. A theme interface the file keeps to itself is not the
			// library's published surface.
			if !isExportedByName(declaration.Modifiers()) {
				return false
			}
			name := declaration.Name()
			if name == nil || name.Kind != ast.KindIdentifier {
				return false
			}

			for _, suffix := range themePropertySuffixes {
				componentPrefix, matched := componentPrefixFromInterfaceName(name.Text(), suffix)
				if !matched {
					continue
				}
				keys := interfacePropertyNames(declaration)
				if len(keys) == 0 {
					continue
				}
				// Accumulate rather than replace, which matters because the tree has two
				// `CalendarTheme.ts` files and one declares a suffix the other does not. Creating
				// the inner map only when absent means a second file contributing `Sizes` does not
				// discard the first file's `Variants`.
				//
				// A same-prefix, same-suffix collision still keeps the last file, and that is the
				// original's behavior rather than a shortfall: its `Object.assign(existing,
				// suffixMap)` copies suffix keys onto the existing map, so distinct suffixes
				// accumulate and a repeated one overwrites. Established by executing the original's
				// merge on both shapes, not by reading it, because the reading admits two
				// interpretations and only one is what `Object.assign` does.
				//
				// Worth knowing rather than fixing: two files declaring the same suffix for one
				// component is a duplicate the library should not have, and silently unioning them
				// would hide that instead of reporting it. Whether the duplicate calendars should
				// both exist is a question for the library rather than for its linter.
				if themes[componentPrefix] == nil {
					themes[componentPrefix] = map[string][]string{}
				}
				themes[componentPrefix][suffix] = keys
			}
			return false
		})
	}
	return themes
}

// isThemeFileName reproduces the original's `^[A-Z]\w*Theme\.ts$` on the base name, scoped to the
// library's source directory.
//
// The capital is load-bearing rather than cosmetic: `useTheme.ts` also ends in `Theme.ts` and is a
// hook rather than a theme. It carries no theme interface, so including it would change nothing
// today, which is exactly why the pattern is reproduced rather than relaxed to a suffix test.
func isThemeFileName(fileName string) bool {
	normalizedPath := strings.ReplaceAll(fileName, `\`, "/")
	if !strings.Contains(normalizedPath, "/libraries/structure/source/") {
		return false
	}
	baseName := normalizedPath[strings.LastIndex(normalizedPath, "/")+1:]
	if !strings.HasSuffix(baseName, "Theme.ts") {
		return false
	}
	if baseName == "" || baseName[0] < 'A' || baseName[0] > 'Z' {
		return false
	}
	// `\w*` between the capital and `Theme`: letters, digits, underscore.
	for index := 1; index < len(baseName)-len("Theme.ts"); index++ {
		character := baseName[index]
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '_':
		default:
			return false
		}
	}
	return true
}

// componentPrefixFromInterfaceName turns `ButtonVariantsInterface` into `Button` for the `Variants`
// suffix.
//
// Both spellings resolve, with and without the trailing `Interface`. The original's comment records
// why: matching only the bare form left the cache empty, because every theme in the tree uses the
// suffixed spelling, and an empty cache disabled the rule silently.
//
// An empty prefix is rejected, which is what keeps a bare `VariantsInterface` from registering a
// component with no name and swallowing every unmatched component into it.
func componentPrefixFromInterfaceName(interfaceName string, suffix string) (string, bool) {
	remainder := strings.TrimSuffix(interfaceName, interfaceNameSuffix)
	if !strings.HasSuffix(remainder, suffix) {
		return "", false
	}
	componentPrefix := strings.TrimSuffix(remainder, suffix)
	if componentPrefix == "" {
		return "", false
	}
	return componentPrefix, true
}

// interfacePropertyNames lists an interface's property names, in declaration order.
func interfacePropertyNames(declaration *ast.InterfaceDeclaration) []string {
	if declaration.Members == nil {
		return nil
	}
	var names []string
	for _, member := range declaration.Members.Nodes {
		if member.Kind != ast.KindPropertySignature {
			continue
		}
		name := member.AsPropertySignatureDeclaration().Name()
		if name == nil {
			continue
		}
		switch name.Kind {
		case ast.KindIdentifier, ast.KindStringLiteral:
			names = append(names, name.Text())
		}
	}
	return names
}
