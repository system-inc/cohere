package boundaries

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// elementDescriptor is one entry of upstream's elements setting, in its default `folder` mode.
type elementDescriptor struct {
	Type    string    `json:"type"`
	Pattern stringSet `json:"pattern"`

	// Capture names the values a pattern's wildcards capture. Accepted and unused: captured values
	// only matter to a selector that matches on them or a message template that prints them, and this
	// port refuses both, so no decision here can depend on a capture.
	Capture []string `json:"capture"`

	// Mode is accepted only as `folder`, upstream's default. `file` and `full` decide membership
	// differently and are refused rather than read as folder.
	Mode string `json:"mode"`
}

// dependencySelector is an allow or disallow entry. Upstream's legacy string form is refused.
type dependencySelector struct {
	From entitySelectors `json:"from"`
	To   entitySelectors `json:"to"`
}

// entitySelector picks the element on one end of a dependency. Upstream also selects by `file` and
// `module`; both are refused.
type entitySelector struct {
	Element *elementSelector `json:"element"`
}

// typesQuery is upstream's array query over an element's types. Only `anyOf` is ported.
type typesQuery struct {
	AnyOf []string `json:"anyOf"`
}

// decodeOneOrMany reads one selector or an array of them, each through rule.UnmarshalOptions.
//
// Every level is decoded strictly here rather than through json.Unmarshal, because a custom
// UnmarshalJSON that calls json.Unmarshal drops the outer decoder's strictness for everything beneath
// it. A selector key this port does not implement, `captured` or `file` or `parent`, would then load
// clean and be ignored, which is a policy narrower than the one written with nothing saying so.
//
// Mutates: into it is the slice an UnmarshalJSON receiver decodes into, so the selectors land on the receiver
func decodeOneOrMany[Item any](raw []byte, into *[]Item, shape string) error {
	// Go whitespace: raw JSON bytes of a rule's options, whose whitespace is the same in both sets.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var many []json.RawMessage
		if decodeError := json.Unmarshal(trimmed, &many); decodeError != nil {
			return decodeError
		}
		for _, element := range many {
			var item Item
			if unmarshalOptionsError := rule.UnmarshalOptions(element, &item); unmarshalOptionsError != nil {
				return fmt.Errorf(
					"%w; this port reads %s, and upstream's legacy string selectors are not ported",
					unmarshalOptionsError,
					shape,
				)
			}
			*into = append(*into, item)
		}
		return nil
	}
	var item Item
	if unmarshalOptionsError := rule.UnmarshalOptions(trimmed, &item); unmarshalOptionsError != nil {
		return fmt.Errorf(
			"%w; this port reads %s, and upstream's legacy string selectors are not ported",
			unmarshalOptionsError,
			shape,
		)
	}
	*into = append(*into, item)
	return nil
}

// decodeDependenciesOptions reads the options and refuses every shape this port does not decide.
//
// A refusal names the key. The alternative, loading a config whose policy this port only partly
// understands, would enforce a narrower boundary than the one written and say nothing about it.
func decodeDependenciesOptions(raw []byte, base rule.OptionsBase) (DependenciesOptions, error) {
	if len(raw) == 0 {
		return DependenciesOptions{}, fmt.Errorf(
			"boundaries/dependencies needs its options: the elements that say which " +
				"folder is which layer, and the policies between them. With neither, no file is an element " +
				"and the rule would decide nothing",
		)
	}

	var settings DependenciesOptions
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &settings); unmarshalOptionsError != nil {
		return DependenciesOptions{}, fmt.Errorf("boundaries/dependencies: %w", unmarshalOptionsError)
	}

	if settings.Default != "" && settings.Default != "allow" && settings.Default != "disallow" {
		return DependenciesOptions{}, fmt.Errorf(
			"boundaries/dependencies: default is %q, and upstream accepts only \"allow\" or \"disallow\"",
			settings.Default,
		)
	}
	for name, flag := range map[string]*bool{
		"checkAllOrigins":    settings.CheckAllOrigins,
		"checkUnknownLocals": settings.CheckUnknownLocals,
		"checkInternals":     settings.CheckInternals,
	} {
		if flag != nil && *flag {
			return DependenciesOptions{}, fmt.Errorf(
				"boundaries/dependencies: %s is true, and this port implements only its "+
					"default, false. Reading it as false would check fewer dependencies than the config asks for",
				name,
			)
		}
	}

	if len(settings.Elements) == 0 {
		return DependenciesOptions{}, fmt.Errorf(
			"boundaries/dependencies has no elements, so no file would belong to a layer " +
				"and the rule would report nothing. Write them under \"elements\", the shape of upstream's " +
				"boundaries/elements setting",
		)
	}
	for index, descriptor := range settings.Elements {
		if descriptor.Type == "" {
			return DependenciesOptions{}, fmt.Errorf("boundaries/dependencies: element %d has no type", index)
		}
		if len(descriptor.Pattern) == 0 {
			return DependenciesOptions{}, fmt.Errorf(
				"boundaries/dependencies: element %q has no pattern",
				descriptor.Type,
			)
		}
		for _, pattern := range descriptor.Pattern {
			if strings.ContainsAny(pattern, "{}()") {
				return DependenciesOptions{}, fmt.Errorf(
					"boundaries/dependencies: element %q has pattern %q, and this port matches "+
						"`*`, `?`, `[...]` and `**` but not micromatch's brace or extglob syntax",
					descriptor.Type,
					pattern,
				)
			}
		}
		if descriptor.Mode != "" && descriptor.Mode != "folder" {
			return DependenciesOptions{}, fmt.Errorf(
				"boundaries/dependencies: element %q has mode %q, and this port implements "+
					"only the default folder mode",
				descriptor.Type,
				descriptor.Mode,
			)
		}
	}

	for index, policy := range settings.Policies {
		if len(policy.Allow) == 0 && len(policy.Disallow) == 0 {
			return DependenciesOptions{}, fmt.Errorf(
				"boundaries/dependencies: policy %d has neither allow nor disallow, so it "+
					"could never decide a dependency",
				index,
			)
		}
		if refuseTemplateError := refuseTemplate(policy.Message); refuseTemplateError != nil {
			return DependenciesOptions{}, fmt.Errorf(
				"boundaries/dependencies: policy %d: %w",
				index,
				refuseTemplateError,
			)
		}
		for _, selectors := range []entitySelectors{policy.From, policy.To} {
			if checkEntitySelectorsError := checkEntitySelectors(selectors); checkEntitySelectorsError != nil {
				return DependenciesOptions{}, fmt.Errorf(
					"boundaries/dependencies: policy %d: %w",
					index,
					checkEntitySelectorsError,
				)
			}
		}
		for _, entries := range []dependencySelectors{policy.Allow, policy.Disallow} {
			for _, entry := range entries {
				for _, selectors := range []entitySelectors{entry.From, entry.To} {
					if checkEntitySelectorsError := checkEntitySelectors(selectors); checkEntitySelectorsError != nil {
						return DependenciesOptions{}, fmt.Errorf(
							"boundaries/dependencies: policy %d: %w",
							index,
							checkEntitySelectorsError,
						)
					}
				}
			}
		}
	}
	if refuseTemplateError := refuseTemplate(settings.Message); refuseTemplateError != nil {
		return DependenciesOptions{}, fmt.Errorf("boundaries/dependencies: %w", refuseTemplateError)
	}

	// Anchored where the config was written, which is where upstream's rootPath sits for every project
	// this runs on: ESLint resolves it from the directory it runs in, and Base runs it from the
	// directory its config lives in.
	settings.Root = base.ConfigDirectory
	if settings.Root == "" {
		settings.Root = base.ProjectRoot
	}
	return settings, nil
}

// checkEntitySelectors refuses a selector that would match by something other than an exact type.
func checkEntitySelectors(selectors entitySelectors) error {
	for _, selector := range selectors {
		if selector.Element == nil {
			return fmt.Errorf("a selector with no element matches every entity upstream knows, by file and " +
				"module as well as element, and only element selectors are ported")
		}
		if selector.Element.Type == "" && selector.Element.Types == nil {
			return fmt.Errorf("an element selector with neither type nor types matches every element")
		}
		names := []string{selector.Element.Type}
		if selector.Element.Types != nil {
			if len(selector.Element.Types.AnyOf) == 0 {
				return fmt.Errorf("types has an empty anyOf, which upstream reads as matching nothing")
			}
			names = append(names, selector.Element.Types.AnyOf...)
		}
		for _, name := range names {
			if strings.ContainsAny(name, "*?[]{}!()+@") {
				return fmt.Errorf("element type %q is a micromatch pattern, and this port matches types "+
					"exactly", name)
			}
		}
	}
	return nil
}

// refuseTemplate refuses a message that upstream would render as a template.
func refuseTemplate(message string) error {
	if strings.Contains(message, "{{") || strings.Contains(message, "${") {
		return fmt.Errorf("the message %q is a template, and this port prints messages verbatim rather "+
			"than rendering upstream's handlebars or legacy placeholders", message)
	}
	return nil
}

// element is a file's place in the element layout: its type, and the folder that is the element.
type element struct {
	Type string
	Path string
}

// describeElement finds which element a root-relative file path belongs to, as @boundaries/elements
// does in folder mode with one type per element, upstream's default.
//
// The walk starts at the file's own name and grows leftwards one segment at a time. At each length,
// every descriptor is tried in order against `pattern/**/*`, and the first to match is the element.
// So the shortest matching suffix wins, which is why upstream documents that a pattern need not be
// written from the root: `source/*` also claims `libraries/base/source/foundation`, because that path
// ends in a suffix the pattern matches. Measured with the installed build rather than taken from its
// documentation, because Base's project boundary depends on it: under that block, Base's own
// foundation files are `project-source` elements and its modules are `project-module` ones.
func describeElement(relativePath string, descriptors []elementDescriptor) (element, bool) {
	segments := strings.Split(relativePath, "/")
	for start := len(segments) - 1; start >= 0; start-- {
		suffix := segments[start:]
		for _, descriptor := range descriptors {
			for _, pattern := range descriptor.Pattern {
				if !globMatches(pattern+"/**/*", suffix) {
					continue
				}
				return element{Type: descriptor.Type, Path: elementPath(pattern, suffix, segments)}, true
			}
		}
	}
	return element{}, false
}

// elementPath is the folder an element is, which is what decides whether two files share one.
//
// Upstream finds the shortest prefix of the matched suffix that the bare pattern matches, then cuts
// the full path at the first place that prefix appears as text. The cut is by text rather than by
// segment, and it is reproduced as text: a path that repeats the matched prefix earlier on would
// resolve to the earlier occurrence upstream, and so it does here.
func elementPath(pattern string, suffix []string, segments []string) string {
	full := strings.Join(segments, "/")
	for length := 1; length <= len(suffix); length++ {
		if !globMatches(pattern, suffix[:length]) {
			continue
		}
		matched := strings.Join(suffix[:length], "/")
		return full[:strings.Index(full, matched)] + matched
	}
	return full
}

// globMatches is micromatch's answer for a slash-separated path, with its default handling of dots.
//
// Element patterns go through micromatch with its defaults, where neither `*` nor `**` matches a
// segment that begins with a dot unless the pattern spells the dot. cohere's config glob matches dot
// segments (config files are matched with `dot: true`), so reusing it would put
// `workers/api/.wrangler/state.ts` inside the `workers/*` element, a membership upstream does not
// grant. It is also not imported for a second reason: a rule package stays a leaf of the build graph
// (`TestRulePackagesStayLeaves`), and the config package is not one.
func globMatches(pattern string, path []string) bool {
	return matchGlobSegments(strings.Split(strings.Trim(pattern, "/"), "/"), path)
}

func matchGlobSegments(pattern []string, segments []string) bool {
	if len(pattern) == 0 {
		return len(segments) == 0
	}
	if pattern[0] == "**" {
		if matchGlobSegments(pattern[1:], segments) {
			return true
		}
		if len(segments) == 0 || strings.HasPrefix(segments[0], ".") {
			return false
		}
		return matchGlobSegments(pattern, segments[1:])
	}
	if len(segments) == 0 {
		return false
	}
	if strings.HasPrefix(segments[0], ".") && !strings.HasPrefix(pattern[0], ".") {
		return false
	}
	// One segment, so `*` and `?` cannot cross a slash. Brace and extglob syntax, which path.Match
	// does not read, is refused when the options are decoded.
	if matched, matchError := path.Match(pattern[0], segments[0]); matchError != nil || !matched {
		return false
	}
	return matchGlobSegments(pattern[1:], segments[1:])
}

// mergeEntitySelectors composes a policy's outer selector with one entry's, as upstream does.
//
// Every outer selector is paired with every entry selector, and within a pair the entry's fields win
// over the outer's. Either side absent leaves the other as it is.
func mergeEntitySelectors(outer entitySelectors, entry entitySelectors) entitySelectors {
	if len(entry) == 0 {
		return outer
	}
	if len(outer) == 0 {
		return entry
	}
	merged := entitySelectors{}
	for _, outerSelector := range outer {
		for _, entrySelector := range entry {
			combined := *outerSelector.Element
			if entrySelector.Element.Type != "" {
				combined.Type = entrySelector.Element.Type
			}
			if entrySelector.Element.Types != nil {
				combined.Types = entrySelector.Element.Types
			}
			merged = append(merged, entitySelector{Element: &combined})
		}
	}
	return merged
}

// selectorsMatch reports whether any selector matches, and an absent side matches anything.
func selectorsMatch(selectors entitySelectors, subject element) bool {
	if len(selectors) == 0 {
		return true
	}
	for _, selector := range selectors {
		if selector.Element.matches(subject) {
			return true
		}
	}
	return false
}

// Dependencies refuses an import that crosses from one element into another its policies do not allow.
//
//	elements: api = libraries/base/source/api/**, foundation = libraries/base/source/foundation/**
//	policies: from foundation allow to api; default disallow
//	valid:   foundation/base/Handler.ts imports '@base/source/api/rpc/client/RpcClient'
//	valid:   api/rpc/client/RpcClient.ts imports './driver/RpcClientDriver'      same element
//	valid:   api/rpc/client/RpcClient.ts imports 'zod'                           not a local file
//	valid:   api/rpc/client/RpcClient.ts imports '@nexus/source/time/DueDate'    no element
//	invalid: api/rpc/client/RpcClient.ts imports '@base/source/foundation/base/Base'
//
// # What is checked, all measured on the installed build
//
// A file that is no element is never checked, and neither is a dependency that resolves to a package,
// to nothing, to a local file in no element, or to a file in the importing file's own element. What
// is left is decided by the policies, last match winning, and then by the default.
//
// A value or type import, a side-effect import, `export * from`, `export { } from` and their
// type-only forms, and `import('…')` with a string argument are each a dependency, as upstream finds
// them. `import x = require('…')` and an `import('…')` type are not, because upstream's selectors do
// not reach them either. The finding lands on the specifier string, where upstream puts it.
//
// # The one divergence, and its size
//
// Upstream also reads `require('…')` called by that bare name. This port does not. The program
// resolves only the specifiers TypeScript collects, and in a TypeScript file it collects no `require`
// call, so the dependency would resolve to nothing; in a JavaScript file it does, but the typed test
// harness builds a TypeScript-only program, so a `require` branch could be written and never shown to
// work, and an unproven branch is not shipped. Measured on api-phi-health: both of Base's blocks lint
// only `.ts`, `.mts` and `.tsx`, and those files hold no `require` call by that bare name (the six
// `require(` matches are `request.require(key)`, which upstream's `callee.name=require` skips too),
// so the divergence reaches nothing there today.
var Dependencies = rule.Rule[DependenciesOptions]{
	Name: "boundaries/dependencies",
	// The project root, and where this file's imports resolve.
	ProgramReads: rule.ReadsCompilerOptions | rule.ReadsModuleResolution,
	Run: func(ruleContext rule.Context, options rule.Given[DependenciesOptions]) rule.Listeners {
		if ruleContext.SourceFile == nil || ruleContext.Program == nil {
			return nil
		}
		settings, configured := options.Read()
		if !configured || len(settings.Elements) == 0 {
			// The decoder refuses an empty element list, so this is a hand-built Context's case, and a
			// skip rather than a silence if it is ever reached in a run (#pa7k7zv).
			ruleContext.Skip("no elements are configured")
			return nil
		}

		root := settings.Root
		if root == "" {
			// A test hands the struct straight in, with no config to anchor it. The program's
			// directory is where its tsconfig sits, which is upstream's rootPath for every project
			// this runs on.
			root = ruleContext.Program.GetCurrentDirectory()
		}
		// The resolver returns a package's file by its real path and a project file by the path it was
		// reached through, so under a root that is itself reached through a link the two disagree:
		// measured in the test harness, `./b` resolved under /tmp and a package under /private/tmp.
		// Measuring only against the root as written would put every package outside the project,
		// where it is skipped for the wrong reason and the external check is never asked.
		roots := []string{root}
		if realRoot, resolveSymlinksError := filepath.EvalSymlinks(
			root,
		); resolveSymlinksError == nil && realRoot != root {
			roots = append(roots, realRoot)
		}
		relative := func(fileName string) (string, bool) {
			for _, candidate := range roots {
				path, relativePathError := filepath.Rel(candidate, fileName)
				// Slashed before the test rather than after it: on Windows Rel answers `..\x`, which the
				// `../` test let through, so every file outside the root was described as inside it.
				path = filepath.ToSlash(path)
				if relativePathError == nil && path != ".." && !strings.HasPrefix(path, "../") {
					return path, true
				}
			}
			return "", false
		}

		fromPath, inside := relative(ruleContext.SourceFile.FileName().AsString())
		if !inside {
			return nil
		}
		from, isElement := describeElement(fromPath, settings.Elements)
		if !isElement {
			return nil
		}

		check := func(specifier *ast.Node) {
			if specifier == nil || specifier.Kind != ast.KindStringLiteral {
				return
			}
			resolved := ruleContext.Program.ResolveModule(ruleContext.SourceFile, specifier)
			if !resolved.IsResolved() || resolved.IsExternalLibraryImport {
				return
			}
			toPath, inside := relative(resolved.ResolvedFileName)
			if !inside {
				return
			}
			to, isElement := describeElement(toPath, settings.Elements)
			if !isElement || to.Path == from.Path {
				return
			}
			if refused, policyIndex := settings.decide(from, to); refused {
				ruleContext.ReportNode(specifier, settings.messageFor(policyIndex, from, to))
			}
		}

		return rule.Listeners{
			ast.KindImportDeclaration: func(node *ast.Node) {
				check(node.AsImportDeclaration().ModuleSpecifier)
			},
			ast.KindExportDeclaration: func(node *ast.Node) {
				check(node.AsExportDeclaration().ModuleSpecifier)
			},
			ast.KindCallExpression: func(node *ast.Node) {
				call := node.AsCallExpression()
				if !ast.IsImportCall(node) || call.Arguments == nil || len(call.Arguments.Nodes) == 0 {
					return
				}
				check(call.Arguments.Nodes[0])
			},
		}
	},
}
