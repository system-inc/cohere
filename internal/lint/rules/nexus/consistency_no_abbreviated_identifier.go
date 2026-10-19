package nexus

import (
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/ecmascript/binding"
	"github.com/system-inc/cohere/internal/lint/ecmascript/imports"
	"github.com/system-inc/cohere/internal/lint/ecmascript/nextjs"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// ConsistencyNoAbbreviatedIdentifierOptions names the files and scopes where a framework owns an
// identifier's spelling.
//
// The rule itself knows nothing about any framework. A consumer names the paths and scopes where a
// framework mandates a name, because only the consumer knows which framework it is running.
// Structure passes Next.js's page, layout, route, and middleware paths; a codebase with no framework
// passes nothing and every identifier is judged on its merits.
type ConsistencyNoAbbreviatedIdentifierOptions struct {
	// FrameworkParameterFilePatterns are path substrings whose files may use framework-mandated
	// parameter names verbatim (`params`, `searchParams`). Next.js demands these spellings in page,
	// layout, and route files, and a rename there breaks the framework contract rather than
	// improving the name.
	FrameworkParameterFilePatterns []string `json:"frameworkParameterFilePatterns"`

	// FrameworkConstantFilePatterns are path substrings whose files may declare a framework-mandated
	// `config` constant. Next.js middleware requires `export const config` by that exact name.
	FrameworkConstantFilePatterns []string `json:"frameworkConstantFilePatterns"`

	// FrameworkParameterScopeNames are exact names of functions, interfaces, or type aliases inside
	// which framework-mandated parameter names are allowed, wherever the file lives. Next.js reads
	// `generateMetadata` and `generateStaticParams` by name.
	FrameworkParameterScopeNames []string `json:"frameworkParameterScopeNames"`

	// FrameworkParameterScopeSuffixes have the same effect, matched against the declaration name and
	// against that name with role suffixes stripped, so `PageRoute` also covers
	// `SomethingPageRouteProperties`.
	FrameworkParameterScopeSuffixes []string `json:"frameworkParameterScopeSuffixes"`
}

// millisecondSegmentPattern matches a millisecond unit written as a camelCase word.
//
// Go's regexp has no lookahead, so where the TypeScript wrote `[a-z]Ms(?=$|[A-Z])` this captures the
// character after instead and puts it back in the replacement. The distinction the pattern is making
// is unchanged: a lowercase letter before, and either the end of the name or the start of the next
// word after.
var millisecondSegmentPattern = regexp.MustCompile(`([a-z])Ms($|[A-Z])`)

// wordSegmentPatterns are the two shapes a mid-name segment can take, plus the replacement form.
type wordSegmentPatterns struct {
	boundary *regexp.Regexp
	camel    *regexp.Regexp
	replace  *regexp.Regexp
}

// The rule's four messages, one per shape a finding takes, whose wording lives in
// `policy/messages/consistency-no-abbreviated-identifier.json`. The reasoning they share is written
// once there as the `reasoning` phrase, because it is the same argument every time. The id names the
// shape, not the word: the word is already in the text, and the vocabulary's entries carry no id.
var (
	abbreviatedIdentifierText  = policy.MessageOf("nexus/consistency-no-abbreviated-identifier", "abbreviatedIdentifier")
	abbreviatedSuffixText      = policy.MessageOf("nexus/consistency-no-abbreviated-identifier", "abbreviatedSuffix")
	millisecondSuffixText      = policy.MessageOf("nexus/consistency-no-abbreviated-identifier", "millisecondSuffix")
	abbreviatedWordSegmentText = policy.MessageOf("nexus/consistency-no-abbreviated-identifier", "abbreviatedWordSegment")
)

func messageNoMsSuffix(name string, suggestion string) rule.Message {
	return rule.Message{
		Id:          millisecondSuffixText.Id,
		Description: millisecondSuffixText.Render(map[string]string{"name": name, "suggestion": suggestion}),
	}
}

func messageNoWordSegment(name string, word string, suggestion string) rule.Message {
	return rule.Message{
		Id:          abbreviatedWordSegmentText.Id,
		Description: abbreviatedWordSegmentText.Render(map[string]string{"name": name, "word": word, "suggestion": suggestion}),
	}
}

// ConsistencyNoAbbreviatedIdentifier rejects abbreviated identifier names in favor of full words.
//
//	valid:   const properties = getProperties()
//	valid:   const configuration = load()
//	valid:   const durationInMilliseconds = 500
//	valid:   import { config } from 'external'
//	invalid: const props = getProperties()
//	invalid: const cfg = 1        (not matched; see the candidate gate)
//	invalid: const idx = 0
//	invalid: const durationMs = 500
//	invalid: const originalInitCwd = process.cwd()
//
// The exemptions carry the judgment, and they divide into two kinds. Some names are not ours: a
// property read, an import specifier, a member of a qualified type name, a JSX tag or attribute. Renaming any of those changes what the code reaches
// for rather than what it calls something. The others are names a framework mandates, which the
// caller names through options because only the caller knows which framework it is running.
// Property declarations in our source and literals defining their own shape are judged. A contextual
// property already declared elsewhere is judged at its declaration, and dependency contracts and
// augmentations are silent. Ownership comes from the checker, never from a property-name list.
//
// No fix, and that is a deliberate departure from the TypeScript original, which renames the
// identifier in place. The original guards that rename with one scope-analysis call,
// `resolvesToImportedBinding`, and that guard's own comment says what it is for: it stops `--fix`
// from renaming a name this file does not own, after `loadConfig` from `tsconfig-paths` lost its
// import exactly that way. This port has no scope analysis, and a rule that reports without renaming
// cannot break an import, so the dependency disappears along with the fix. The suggested name
// travels in the message instead, where a reader applies it with the scope in front of them. Four
// other ported rules made the same call for the same reason.
//
// The original's scope walk also kept it silent on every reference to an imported name, and this
// port reported those until api-phi-health's parity sweep counted six (#dx1vrfm). It now skips a
// reference to a name an import binds and nothing else in the file declares, which agrees with the
// scope walk everywhere but a file that shadows an import; there it keeps reporting, erring toward a
// finding. The rule judges names the code declares, never names it reads from elsewhere.
var ConsistencyNoAbbreviatedIdentifier = rule.Rule{
	Name:             "nexus/consistency-no-abbreviated-identifier",
	NeedsTypeChecker: true,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, _ := rule.OptionsAs[ConsistencyNoAbbreviatedIdentifierOptions](options)

		fileName := imports.NormalizedFileName(ctx.SourceFile)
		isFrameworkParameterFile := matchesAnyFilePattern(fileName, settings.FrameworkParameterFilePatterns)
		isFrameworkConstantFile := matchesAnyFilePattern(fileName, settings.FrameworkConstantFilePatterns)
		routeContract := nextjs.RouteContractExports(fileName)
		// Built on the first candidate identifier, so a file with no abbreviated name never walks.
		var importedNames map[string]bool
		// Answered on the first `args`, so no other file walks for it.
		argumentsNameRestOnly, argumentsBindingsRead := false, false

		judge := func(node *ast.Node) {
			name := node.Text()
			if node.Kind == ast.KindPrivateIdentifier {
				name = strings.TrimPrefix(name, "#")
			}

			// Cheapest question first: could this name match any branch at all? Everything below
			// is a skip or a report keyed on the same spellings, so a name no branch could match
			// reports nothing whichever order the guards run in. See abbreviation_gate.go.
			if !isAbbreviationCandidate(name) {
				return
			}

			property := abbreviatedPropertyMember(node) != nil
			if property {
				if !abbreviatedOwnsProperty(ctx, node) {
					return
				}
			} else if node.Kind != ast.KindIdentifier || binding.IsForeignName(node) ||
				(node.Parent != nil && node.Parent.Kind == ast.KindPropertyAccessExpression && node.Parent.Name() == node) {
				return
			}

			// A reference to an imported name is the other module's spelling, as the import
			// specifier is: `loadConfig(directory)` and drizzle's `char(name)` read names this
			// file does not own (#dx1vrfm). The original skips them through a scope walk; this
			// port has none, so a name counts as imported when an import binds it and nothing
			// else in the file declares it. A file that shadows an import keeps reporting the
			// name everywhere, which errs toward a finding rather than away from one.
			if importedNames == nil {
				importedNames = importedNamesNeverRedeclared(ctx.SourceFile)
			}
			if !property && importedNames[name] {
				return
			}

			// A name Next.js reads off this route file by name is the framework's spelling,
			// not ours, and the options above cannot be trusted to list it: `maxDuration`
			// passes the `max[A-Z]` gate and no consumer named it, so this rule told route
			// authors to write `maximumDuration`, which Next then silently ignores. That is the
			// shape of the rename that cost five phi web routes their prerendering, so the
			// floor comes from `nextjs.IsRouteContractExport` whatever the consumer passes.
			//
			// Every occurrence of the spelling is exempt, the export and its references alike,
			// except a nested binding that shadows it: a `const config` inside a function is
			// this file's own name again and is judged on its merits.
			if !property && routeContract[name] && !declaresNestedBinding(node) {
				return
			}

			// The vocabulary says what the word is; policy/Abbreviations.json holds it, and Swift
			// compiles in the same file. What follows is policy, which stays here.
			finding, found := vocabulary.find(name)
			if !found {
				return
			}

			// Spellings a framework or a convention mandates, each silencing exactly the finding
			// it names. Each was a `return` at that branch when the vocabulary was code, so
			// matching on the finding's form and word is the same decision in the same place.
			switch finding.form + " " + finding.entry.Abbreviation {
			case "whole params", "suffix params":
				// Next.js requires `params` and `searchParams` verbatim in page, layout, and
				// route files, and inside the functions it reads by name.
				if !property && (isFrameworkParameterFile || isInsideFrameworkScope(node, settings)) {
					return
				}
			case "whole ref":
				// React 19 made `ref` a regular property on function components. The canonical
				// name is load-bearing: interface keys, destructured shorthand, and
				// forward-into-child JSX attribute values all have to read as `ref` for consumers
				// to wire refs up correctly.
				if !property && isReactReferencePropertyContext(node) {
					return
				}
			case "whole config":
				// Next.js middleware requires `export const config` by that exact name.
				if !property && isFrameworkConstantFile {
					return
				}
			case "whole args":
				// A rest parameter in a variadic or framework function keeps the conventional
				// spelling: `...args` is the shape everyone reads.
				//
				// Its uses have to agree with it. The name was chosen once, at the exempt rest,
				// so reporting `target(...args)` put the finding away from where anyone could act
				// on it, and only there (#e000k8d). Without scope analysis a use cannot be traced
				// to its binding, so the question is asked of the file, the same way imported
				// names are: when every `args` the file binds is a rest, every `args` is exempt.
				// A file that also binds a plain parameter or catch binding named `args` keeps
				// reporting all of them, erring toward a finding. The ESLint twin asks the file
				// the same question, so the two engines agree.
				if isRestElement(node) {
					return
				}
				if !argumentsBindingsRead {
					argumentsNameRestOnly = nameBoundOnlyAsRest(ctx.SourceFile, "args")
					argumentsBindingsRead = true
				}
				if !property && argumentsNameRestOnly {
					return
				}
			}

			// The identifier's own text, not its leading trivia.
			//
			// `node.Loc.Pos()` sits before the trivia, so a binding preceded by a comment
			// reports at the comment. That is the one line an `eslint-disable-next-line` above
			// it cannot cover, since the directive matches the line after itself: `params` in
			// McpApi.ts reported at line 240 while its suppression sat on 241 covering 242, so a
			// correctly-suppressed identifier still produced a finding nothing could silence.
			ctx.ReportRange(rule.TokenRange(ctx.SourceFile, node), finding.message)
		}
		return rule.Listeners{ast.KindIdentifier: judge, ast.KindStringLiteral: judge, ast.KindPrivateIdentifier: judge}
	},
}

// replaceFirst replaces only the first match, which is what JavaScript's non-global String.replace
// does and what every suggestion in this rule means.
//
// Go's ReplaceAllString would rewrite every occurrence, so `variablesForVars` would come back
// double-renamed while the original renamed one segment and left the reader to judge the rest.
func replaceFirst(pattern *regexp.Regexp, text string, replacement string) string {
	location := pattern.FindStringSubmatchIndex(text)
	if location == nil {
		return text
	}
	expanded := pattern.ExpandString(nil, replacement, text, location)
	return text[:location[0]] + string(expanded) + text[location[1]:]
}

// matchesAnyFilePattern reports whether the linted file matches any caller-supplied path substring.
func matchesAnyFilePattern(fileName string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(fileName, pattern) {
			return true
		}
	}
	return false
}

// isRestElement reports whether an identifier is the name bound by a rest parameter or a rest
// binding element, the `...args` shape.
func isRestElement(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindParameter:
		parameter := parent.AsParameterDeclaration()
		return parameter != nil && parameter.DotDotDotToken != nil && parameter.Name() == node
	case ast.KindBindingElement:
		element := parent.AsBindingElement()
		return element != nil && element.DotDotDotToken != nil && element.Name() == node
	}
	return false
}

// isReactReferencePropertyContext reports whether a `ref` identifier is participating in React 19's
// ref-as-property pattern.
//
// React 19 made `ref` a regular property on plain function components, so the canonical name became
// load-bearing: any other spelling silently breaks ref forwarding for consumers. Three positions
// count, matching the original: an interface member, a shorthand in a destructure, and an
// expression-position reference inside a function whose parameter destructures `ref`.
func isReactReferencePropertyContext(node *ast.Node) bool {
	if node.Text() != "ref" {
		return false
	}
	parent := node.Parent
	if parent == nil {
		return false
	}

	// An interface or type-literal member: `ref?: React.Ref<T>`. Already exempt via the property
	// signature case above, kept here so this predicate answers the whole question on its own.
	if parent.Kind == ast.KindPropertySignature {
		signature := parent.AsPropertySignatureDeclaration()
		if signature != nil && signature.Name() == node {
			return true
		}
	}

	// A shorthand in an object literal or a binding pattern: `{ ref }`.
	if parent.Kind == ast.KindShorthandPropertyAssignment {
		return true
	}
	if parent.Kind == ast.KindBindingElement {
		element := parent.AsBindingElement()
		if element != nil && element.Name() == node {
			return true
		}
	}

	// An expression-position reference inside a function that destructures `ref` from its
	// properties object, which is the forwarding case: `<Child ref={ref} />`.
	for current := parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
			if functionDestructuresReference(current) {
				return true
			}
		}
	}
	return false
}

// functionDestructuresReference reports whether any parameter of a function destructures `ref` at
// the top level of an object pattern.
//
// Only the top level, because React 19's ref-as-property only ever sits there.
func functionDestructuresReference(functionNode *ast.Node) bool {
	parameters := functionNode.Parameters()
	for _, parameterNode := range parameters {
		parameter := parameterNode.AsParameterDeclaration()
		if parameter == nil {
			continue
		}
		binding := parameter.Name()
		if binding == nil || binding.Kind != ast.KindObjectBindingPattern {
			continue
		}
		pattern := binding.AsBindingPattern()
		if pattern == nil || pattern.Elements == nil {
			continue
		}
		for _, elementNode := range pattern.Elements.Nodes {
			element := elementNode.AsBindingElement()
			if element == nil {
				continue
			}
			elementName := element.Name()
			if elementName != nil && elementName.Kind == ast.KindIdentifier && elementName.Text() == "ref" {
				return true
			}
		}
	}
	return false
}

// isInsideFrameworkScope reports whether an identifier sits inside a declaration the framework owns.
//
// A path pattern alone is not enough. Next.js reads `generateMetadata` and `generateStaticParams` by
// name wherever they are declared, and a project may route through a component named `*PageRoute`
// that lives outside the app directory entirely. In both cases the enclosing declaration, not the
// filename, is what makes the spelling mandatory, so this checks the enclosing function and the
// enclosing interface or type alias.
//
// A type name is matched after stripping the role suffixes our conventions append, so that
// `PublicProfilePageRouteProperties` matches a `PageRoute` suffix the same way the function does.
func isInsideFrameworkScope(node *ast.Node, settings ConsistencyNoAbbreviatedIdentifierOptions) bool {
	if len(settings.FrameworkParameterScopeNames) == 0 && len(settings.FrameworkParameterScopeSuffixes) == 0 {
		return false
	}

	matches := func(name string) bool {
		for _, scopeName := range settings.FrameworkParameterScopeNames {
			if name == scopeName {
				return true
			}
		}
		if len(settings.FrameworkParameterScopeSuffixes) == 0 {
			return false
		}
		baseName := name
		for _, roleSuffix := range []string{"Properties", "Interface", "Options", "Type"} {
			baseName = strings.TrimSuffix(baseName, roleSuffix)
		}
		for _, suffix := range settings.FrameworkParameterScopeSuffixes {
			if strings.HasSuffix(name, suffix) || strings.HasSuffix(baseName, suffix) {
				return true
			}
		}
		return false
	}

	if functionName, hasName := enclosingFunctionName(node); hasName && matches(functionName) {
		return true
	}
	if typeName, hasName := enclosingTypeDeclarationName(node); hasName && matches(typeName) {
		return true
	}
	return false
}

// enclosingFunctionName walks up to the nearest function and returns its name.
//
// The walk stops at the first function rather than continuing past it, matching the original: a name
// mandated by an outer scope does not reach through an inner function that chose its own.
func enclosingFunctionName(node *ast.Node) (string, bool) {
	for current := node.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindFunctionDeclaration:
			if name := current.Name(); name != nil {
				return name.Text(), true
			}
			return "", false

		case ast.KindFunctionExpression, ast.KindArrowFunction:
			// An arrow or function expression assigned to a variable takes the variable's name.
			parent := current.Parent
			if parent != nil && parent.Kind == ast.KindVariableDeclaration {
				declaration := parent.AsVariableDeclaration()
				if declaration != nil {
					if name := declaration.Name(); name != nil && name.Kind == ast.KindIdentifier {
						return name.Text(), true
					}
				}
			}
			return "", false
		}
	}
	return "", false
}

// enclosingTypeDeclarationName walks up to the nearest interface or type alias and returns its name.
func enclosingTypeDeclarationName(node *ast.Node) (string, bool) {
	for current := node.Parent; current != nil; current = current.Parent {
		switch current.Kind {
		case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
			if name := current.Name(); name != nil {
				return name.Text(), true
			}
			return "", false
		}
	}
	return "", false
}

// declaresNestedBinding reports whether an identifier is the name of a binding declared below module
// scope: a parameter, a destructured element, or a variable, function or class inside a function or
// block.
//
// A Next.js contract export is a module-scope name. A nested binding with the same spelling is a
// different variable that only shares the letters, so it gets no contract exemption.
func declaresNestedBinding(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil || parent.Name() != node {
		return false
	}
	switch parent.Kind {
	case ast.KindParameter, ast.KindBindingElement:
		return true
	case ast.KindFunctionDeclaration, ast.KindClassDeclaration:
		return parent.Parent == nil || parent.Parent.Kind != ast.KindSourceFile
	case ast.KindVariableDeclaration:
		// Declaration, then its list, then the statement holding the list.
		list := parent.Parent
		if list == nil || list.Parent == nil {
			return true
		}
		statement := list.Parent
		return statement.Parent == nil || statement.Parent.Kind != ast.KindSourceFile
	}
	return false
}

// nameBoundOnlyAsRest reports whether a file binds a name at least once and only ever as a rest:
// `...args` in a parameter list or `[first, ...args]` in a destructuring pattern.
//
// A binding is counted by the same test importedNamesNeverRedeclared uses, an identifier that is its
// parent's name, less the members and keys that bind nothing in scope. An import counts as a binding
// that is not a rest, though the import skip has already answered for an imported name.
func nameBoundOnlyAsRest(sourceFile *ast.SourceFile, name string) bool {
	bound, onlyRest := false, true
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if !onlyRest {
			return true
		}
		if node.Kind == ast.KindIdentifier && node.Text() == name && node.Parent != nil && node.Parent.Name() == node {
			switch node.Parent.Kind {
			case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindPropertyAccessExpression,
				ast.KindPropertySignature, ast.KindPropertyDeclaration, ast.KindMethodDeclaration,
				ast.KindMethodSignature, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindExportSpecifier,
				ast.KindEnumMember, ast.KindJsxAttribute:
				// Members and keys bind no name in scope.
			default:
				bound = true
				if !isRestElement(node) {
					onlyRest = false
					return true
				}
			}
		}
		node.ForEachChild(visit)
		return !onlyRest
	}
	sourceFile.AsNode().ForEachChild(visit)
	return bound && onlyRest
}

// importedNamesNeverRedeclared returns the names an import in this file binds, less any name the
// file also declares somewhere: a variable, a parameter, a destructured element, a function, a class
// or any other named declaration. Such a name may be shadowed, and without scope analysis a reference
// cannot be told apart, so it is left to be judged.
func importedNamesNeverRedeclared(sourceFile *ast.SourceFile) map[string]bool {
	imported := map[string]bool{}
	declared := map[string]bool{}
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		if node.Kind == ast.KindIdentifier && node.Parent != nil && node.Parent.Name() == node {
			switch node.Parent.Kind {
			case ast.KindImportClause, ast.KindNamespaceImport, ast.KindImportSpecifier, ast.KindImportEqualsDeclaration:
				imported[node.Text()] = true
			case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindPropertyAccessExpression,
				ast.KindPropertySignature, ast.KindPropertyDeclaration, ast.KindMethodDeclaration,
				ast.KindMethodSignature, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindExportSpecifier,
				ast.KindEnumMember, ast.KindJsxAttribute:
				// Members and keys bind no name in scope.
			default:
				declared[node.Text()] = true
			}
		}
		node.ForEachChild(visit)
		return false
	}
	sourceFile.AsNode().ForEachChild(visit)
	for name := range declared {
		delete(imported, name)
	}
	return imported
}

// abbreviatedPropertyMember separates property declarations from reads and binding names.
// A computed string literal is a known key; other computed expressions remain references.
func abbreviatedPropertyMember(node *ast.Node) *ast.Node {
	if node == nil {
		return nil
	}
	key := node
	parent := node.Parent
	if parent != nil && parent.Kind == ast.KindComputedPropertyName && node.Kind == ast.KindStringLiteral {
		key = parent
		parent = parent.Parent
	}
	if parent == nil || parent.Name() != key {
		return nil
	}
	switch parent.Kind {
	case ast.KindPropertySignature, ast.KindMethodSignature, ast.KindPropertyDeclaration,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment:
		return parent
	}
	return nil
}

func abbreviatedOwnsProperty(ctx rule.Context, name *ast.Node) bool {
	member := abbreviatedPropertyMember(name)
	owner := member.Parent
	if owner == nil {
		return false
	}
	if owner.Kind == ast.KindObjectLiteralExpression {
		if ctx.TypeChecker == nil {
			return true
		}
		contextual := ctx.TypeChecker.GetContextualType(owner, checker.ContextFlagsIgnoreNodeInferences)
		return !abbreviatedContextDeclaresProperty(ctx, contextual, name.Text(), owner)
	}
	if abbreviatedExternalDeclaration(member) {
		return false
	}
	for current := member; current != nil; current = current.Parent {
		if current.Kind == ast.KindModuleDeclaration &&
			(ast.IsGlobalScopeAugmentation(current) || current.Name().Kind == ast.KindStringLiteral) {
			return false
		}
	}
	if ctx.TypeChecker == nil {
		return true
	}
	// Declaration merging can add a member to a foreign interface without a module wrapper.
	if owner.Name() != nil && abbreviatedExternalSymbol(ctx.TypeChecker.GetSymbolAtLocation(owner.Name())) {
		return false
	}
	return !abbreviatedInheritedExternal(ctx, owner, name.Text(), ast.IsStatic(member), make(map[*ast.Node]bool))
}

// A local override can still carry a dependency's contract through another local override.
// Follow the declarations behind inherited properties, with a cycle guard for erroneous heritage.
func abbreviatedInheritedExternal(ctx rule.Context, owner *ast.Node, name string, static bool, seen map[*ast.Node]bool) bool {
	if seen[owner] {
		return false
	}
	seen[owner] = true
	heritage := abbreviatedHeritage(owner)
	if heritage == nil {
		return false
	}
	for _, clause := range heritage.Nodes {
		if static && clause.AsHeritageClause().Token == ast.KindImplementsKeyword {
			continue
		}
		for _, base := range clause.AsHeritageClause().Types.Nodes {
			baseType := ctx.TypeChecker.GetTypeAtLocation(base)
			if static {
				baseType = ctx.TypeChecker.GetTypeAtLocation(base.Expression())
			}
			if baseType == nil {
				continue
			}
			symbol := ctx.TypeChecker.GetPropertyOfType(baseType, name)
			if abbreviatedExternalSymbol(symbol) {
				return true
			}
			if symbol == nil {
				continue
			}
			for _, declaration := range symbol.Declarations {
				if declaration.Parent != nil && abbreviatedInheritedExternal(ctx, declaration.Parent, name, static, seen) {
					return true
				}
			}
		}
	}
	return false
}

// A contextual type may be a union whose alternatives do not share the property. A named
// declaration in any alternative still owns that spelling; a synthetic index/mapped property
// without a declaration of this name does not. Inferred self-context is not an earlier declaration.
func abbreviatedContextDeclaresProperty(ctx rule.Context, contextual *checker.Type, name string, literal *ast.Node) bool {
	if contextual == nil {
		return false
	}
	if contextual.Flags()&checker.TypeFlagsUnionOrIntersection != 0 {
		for _, part := range contextual.Types() {
			if abbreviatedContextDeclaresProperty(ctx, part, name, literal) {
				return true
			}
		}
		return false
	}
	symbol := ctx.TypeChecker.GetPropertyOfType(contextual, name)
	if symbol == nil {
		return false
	}
	for _, declaration := range symbol.Declarations {
		key := declaration.Name()
		if abbreviatedPropertyMember(key) == declaration && declaration.Parent != literal {
			return true
		}
	}
	return false
}

func abbreviatedExternalSymbol(symbol *ast.Symbol) bool {
	if symbol == nil {
		return false
	}
	for _, declaration := range symbol.Declarations {
		if abbreviatedExternalDeclaration(declaration) {
			return true
		}
	}
	return false
}

func abbreviatedExternalDeclaration(declaration *ast.Node) bool {
	file := ast.GetSourceFileOfNode(declaration)
	return file != nil && (file.IsDeclarationFile || strings.Contains(strings.ReplaceAll(file.FileName().AsString(), "\\", "/"), "/node_modules/"))
}

func abbreviatedHeritage(owner *ast.Node) *ast.NodeList {
	switch owner.Kind {
	case ast.KindInterfaceDeclaration:
		return owner.AsInterfaceDeclaration().HeritageClauses
	case ast.KindClassDeclaration:
		return owner.AsClassDeclaration().HeritageClauses
	case ast.KindClassExpression:
		return owner.AsClassExpression().HeritageClauses
	}
	return nil
}
