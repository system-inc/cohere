package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A rule declares exactly what it reads through ctx.Program, and this is what makes that structural
// rather than remembered.
//
// The findings cache decides from the declaration (rule.ProgramReads) whether a rule's findings can be
// replayed and on what key: compiler options and the default library are in the key already, this
// file's module resolution is in the type fingerprint, and anything else keeps the rule out of the
// cache (#b8k3bp6). An undeclared read serves a stale result when what it read changes: zero findings,
// forever, indistinguishable from a clean tree. An overdeclared read keeps a rule out of the cache it
// could have joined. So both directions fail here.
//
// ctx.Program refuses an undeclared read at runtime too, but only on the paths a run happens to take.
// This reads each rule file's syntax, so it covers every path:
//
//   - every method a rule calls on its context's Program, through any chain (`state.ctx.Program`);
//   - every function a rule hands its Program to, by what that function reads, followed through the
//     helpers in the rule packages and in `checking` to a fixed point;
//   - a Program handed to a function this cannot see is refused, since its reads cannot be counted.
//
// Text was what this checked until #ym4v8bc found two holes in it, both in the Tailwind rules. A
// helper that reads the program through a rule.Context hides the read from its callers, so a file that
// reads the program through a context and defines no rule still fails here: such a helper takes a
// rule.Program, and every call site then names ctx.Program. And a declaration in a comment is not a
// declaration: only a ProgramReads key in a composite literal counts.
func TestRulesDeclareExactlyWhatTheyReadOfTheProgram(test *testing.T) {
	test.Parallel()

	ruleFiles := ruleSourceFiles(test)
	if len(ruleFiles) == 0 {
		// A check that found nothing to check passes for the wrong reason, which is the same shape
		// as the defect it guards against.
		test.Fatal("found no rule source files, so this test proved nothing")
	}
	helpers := programHelperReads(test, append(append(append([]string{}, ruleFiles...), checkingSourceFiles(test)...),
		ruleInterfaceSourceFiles(test)...))
	contextFields := contextFieldsByDirectory(test, ruleFiles)

	sawProgramReader := false
	for _, path := range ruleFiles {
		facts := readProgramFacts(test, path, helpers, contextFields[filepath.Dir(path)])
		if !facts.readsProgram {
			continue
		}
		sawProgramReader = true

		if !facts.definesRule {
			test.Errorf("%s reads the program through a rule.Context and defines no rule, so the rules "+
				"calling it read the program without naming it and nothing makes them declare it. "+
				"Take a rule.Program instead, so each call site names ctx.Program", path)
			continue
		}
		for _, unseen := range facts.unseenCallees {
			test.Errorf("%s hands its Program to %s, which this cannot read, so its reads cannot be counted",
				path, unseen)
		}
		if missing := facts.reads &^ facts.declared; missing != 0 {
			test.Errorf("%s reads %s through ctx.Program and does not declare it (declares %s), so a findings "+
				"cache would replay its findings after what it read had changed", path, missing, facts.declared)
		}
		if extra := facts.declared &^ facts.reads; extra != 0 {
			test.Errorf("%s declares %s and reads none of it, which keeps it out of a cache it could join; "+
				"declare %s", path, extra, facts.reads)
		}
	}

	if !sawProgramReader {
		// Dozens of rules read the program today. Zero would mean the search is looking in the wrong
		// place rather than that the tree got cleaner.
		test.Fatal("no rule source reads ctx.Program, which is implausible: this check is " +
			"looking at the wrong files")
	}
}

// programRead mirrors rule.ProgramRead. This package reads rule sources as syntax and does not link
// the rules, so it names the kinds the way the sources spell them.
type programRead uint8

const (
	readsCompilerOptions programRead = 1 << iota
	readsDefaultLibrary
	readsModuleResolution
	readsOtherFiles
	readsDesignSystem
)

// The check sees code and not text, and counts reads by kind, in every direction it could be wrong.
//
// Each source is a shape this guard could misjudge, written to a file and read the way the guard
// reads the tree, so the guard is shown able to fail rather than only shown passing.
func TestReadProgramFactsSeesCodeNotComments(test *testing.T) {
	test.Parallel()

	helpers := map[string]programRead{"type_checking.IsPromiseLike": readsCompilerOptions | readsDefaultLibrary,
		"probe.fingerprint": readsCompilerOptions}
	testCases := []struct {
		name   string
		source string
		// sibling is another file of the probe's package, when the shape spans two.
		sibling string
		want    programFacts
	}{
		{
			// The shape DesignSystemForProgram had: a helper reading the program for its callers.
			name: "a helper reading the program through a context",
			source: `package probe
func helper(ctx rule.Context) bool { return ctx.Program.Options() != nil }`,
			want: programFacts{readsProgram: true, reads: readsCompilerOptions},
		},
		{
			name: "a declaration in a comment",
			source: `package probe
// Every caller must declare ` + "`ProgramReads: rule.ReadsOtherFiles`" + `.
var Probe = rule.Rule[rule.NoOptions]{Run: func(ctx rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { _ = ctx.Program.SourceFiles(); return nil }}`,
			want: programFacts{readsProgram: true, definesRule: true, reads: readsOtherFiles},
		},
		{
			name: "a read in a comment only",
			source: `package probe
// This rule never touches ctx.Program.
var Probe = rule.Rule[rule.NoOptions]{Run: func(ctx rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { return nil }}`,
			want: programFacts{definesRule: true},
		},
		{
			// A context under another name, and reached through a struct, is still the context.
			name: "a context under another name, through a chain",
			source: `package probe
var Probe = rule.Rule[rule.NoOptions]{ProgramReads: rule.ReadsModuleResolution, Run: func(c rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { _ = state{c}.c.Program.ResolveModule(nil, nil); return nil }}`,
			want: programFacts{
				readsProgram: true,
				definesRule:  true,
				reads:        readsModuleResolution,
				declared:     readsModuleResolution,
			},
		},
		{
			// A helper reading the program through a field declared in another file of its package, the
			// shape a walker split from its rule has: the field's type says it is the context, whatever
			// it is called.
			name: "a context field declared in another file of the package",
			source: `package probe
func (walker probeWalker) helper() bool { return walker.ruleContext.Program.Options() != nil }`,
			sibling: `package probe
type probeWalker struct { ruleContext rule.Context }`,
			want: programFacts{readsProgram: true, reads: readsCompilerOptions},
		},
		{
			// The same name on a field of another type is not the context.
			name: "a field of another type under a context's name",
			source: `package probe
func (walker probeWalker) helper() bool { return walker.ruleContext.Program.Options() != nil }`,
			sibling: `package probe
type probeWalker struct { ruleContext probeState }`,
			want: programFacts{},
		},
		{
			// A read through a helper counts as the helper's reads.
			name: "a read through a helper",
			source: `package probe
var Probe = rule.Rule[rule.NoOptions]{ProgramReads: rule.ReadsCompilerOptions, Run: func(ctx rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { _ = type_checking.IsPromiseLike(ctx.Program, nil, nil); return nil }}`,
			want: programFacts{
				readsProgram: true,
				definesRule:  true,
				reads:        readsCompilerOptions | readsDefaultLibrary,
				declared:     readsCompilerOptions,
			},
		},
		{
			// The walk calls a rule's ProgramFingerprint with the rule's own view of the program, so its
			// reads count as the rule's.
			name: "a read through the program fingerprint",
			source: `package probe
var Probe = rule.Rule[rule.NoOptions]{ProgramReads: rule.ReadsCompilerOptions, ProgramFingerprint: fingerprint, Run: func(ctx rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { return nil }}`,
			want: programFacts{
				readsProgram: true,
				definesRule:  true,
				reads:        readsCompilerOptions,
				declared:     readsCompilerOptions,
			},
		},
		{
			name: "a program fingerprint this cannot see",
			source: `package probe
var Probe = rule.Rule[rule.NoOptions]{ProgramFingerprint: elsewhere, Run: func(ctx rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { return nil }}`,
			want: programFacts{readsProgram: true, definesRule: true, unseenCallees: []string{"probe.elsewhere"}},
		},
		{
			name: "a Program handed to a function this cannot see",
			source: `package probe
var Probe = rule.Rule[rule.NoOptions]{Run: func(ctx rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { somewhere.Else(ctx.Program); return nil }}`,
			want: programFacts{readsProgram: true, definesRule: true, unseenCallees: []string{"somewhere.Else"}},
		},
	}
	for _, testCase := range testCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			directory := test.TempDir()
			path := filepath.Join(directory, "probe.go")
			if writeError := os.WriteFile(path, []byte(testCase.source), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			paths := []string{path}
			if testCase.sibling != "" {
				siblingPath := filepath.Join(directory, "sibling.go")
				if writeError := os.WriteFile(siblingPath, []byte(testCase.sibling), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
				paths = append(paths, siblingPath)
			}
			got := readProgramFacts(test, path, helpers, contextFieldsByDirectory(test, paths)[directory])
			if got.readsProgram != testCase.want.readsProgram || got.definesRule != testCase.want.definesRule ||
				got.reads != testCase.want.reads || got.declared != testCase.want.declared ||
				strings.Join(got.unseenCallees, ",") != strings.Join(testCase.want.unseenCallees, ",") {
				test.Errorf("got %+v, want %+v", got, testCase.want)
			}
		})
	}
}

func (reads programRead) String() string {
	names := []string{}
	for read, name := range map[programRead]string{
		readsCompilerOptions:  "ReadsCompilerOptions",
		readsDefaultLibrary:   "ReadsDefaultLibrary",
		readsModuleResolution: "ReadsModuleResolution",
		readsOtherFiles:       "ReadsOtherFiles",
		readsDesignSystem:     "ReadsDesignSystem",
	} {
		if reads&read != 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, " | ")
}

// programReadsByConstant is each declaration constant as a source spells it.
var programReadsByConstant = map[string]programRead{
	"ReadsCompilerOptions":  readsCompilerOptions,
	"ReadsDefaultLibrary":   readsDefaultLibrary,
	"ReadsModuleResolution": readsModuleResolution,
	"ReadsOtherFiles":       readsOtherFiles,
	"ReadsDesignSystem":     readsDesignSystem,
}

// programReadsByMethod is what each rule.Program method reads. It must name every method the
// interface has, which the build cannot check from here, so a method missing from this table is
// refused as unseen rather than counted as reading nothing.
var programReadsByMethod = map[string]programRead{
	"Options":                        readsCompilerOptions,
	"GetCurrentDirectory":            readsCompilerOptions,
	"UseCaseSensitiveFileNames":      readsCompilerOptions,
	"IsSourceFileDefaultLibrary":     readsDefaultLibrary,
	"DefaultLibraryPath":             readsDefaultLibrary,
	"ResolveModule":                  readsModuleResolution,
	"GetSourceFileForResolvedModule": readsOtherFiles,
	"SourceFiles":                    readsOtherFiles,
	"GetSourceFile":                  readsOtherFiles,
	"FS":                             readsOtherFiles,
	"DesignSystemFS":                 readsDesignSystem,
	"Identity":                       0,
}

// programFacts is what one rule file does with the program, read from its syntax.
type programFacts struct {
	// readsProgram is a `.Program` selector on a rule.Context, under any name and through any chain.
	readsProgram bool
	// definesRule is a rule.Rule composite literal.
	definesRule bool
	// reads is what the file reads through its context's Program, directly and through helpers.
	reads programRead
	// declared is the file's ProgramReads key.
	declared programRead
	// unseenCallees are functions handed the Program whose reads this cannot count.
	unseenCallees []string
}

// programUse is one place a Program value is used: a method called on it, or a function it is passed
// to, named `package.Function` in the caller's terms.
type programUse struct {
	methods []string
	callees []string
}

// usesOf collects how the Program values isProgram picks out are used inside node.
func usesOf(node ast.Node, packageName string, isProgram func(ast.Expr) bool) programUse {
	uses := programUse{}
	ast.Inspect(node, func(inner ast.Node) bool {
		call, isCall := inner.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && isProgram(selector.X) {
			uses.methods = append(uses.methods, selector.Sel.Name)
		}
		for _, argument := range call.Args {
			if !isProgram(argument) {
				continue
			}
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				uses.callees = append(uses.callees, packageName+"."+callee.Name)
			case *ast.SelectorExpr:
				if qualifier, isIdentifier := callee.X.(*ast.Ident); isIdentifier {
					uses.callees = append(uses.callees, qualifier.Name+"."+callee.Sel.Name)
				} else {
					uses.callees = append(uses.callees, "(method)."+callee.Sel.Name)
				}
			default:
				uses.callees = append(uses.callees, "(an expression)")
			}
		}
		return true
	})
	return uses
}

// programHelperReads is what every function taking a rule.Program reads of it, followed through the
// functions it hands the Program on to, to a fixed point. Keyed `package.Function`.
func programHelperReads(test *testing.T, paths []string) map[string]programRead {
	test.Helper()
	uses := map[string]programUse{}
	for _, path := range paths {
		file, parseFileError := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseFileError != nil {
			test.Fatalf("parsing %s: %v", path, parseFileError)
		}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil || function.Recv != nil {
				continue
			}
			names := map[string]bool{}
			for _, field := range function.Type.Params.List {
				// Inside the rule package itself the type is spelled bare (rule.FingerprintPath).
				bare, isBare := field.Type.(*ast.Ident)
				if isRuleSelector(field.Type, "Program") ||
					(file.Name.Name == "rule" && isBare && bare.Name == "Program") {
					for _, name := range field.Names {
						names[name.Name] = true
					}
				}
			}
			if len(names) == 0 {
				continue
			}
			isProgram := func(expression ast.Expr) bool {
				identifier, isIdentifier := expression.(*ast.Ident)
				return isIdentifier && names[identifier.Name]
			}
			uses[file.Name.Name+"."+function.Name.Name] = usesOf(function.Body, file.Name.Name, isProgram)
		}
	}

	reads := map[string]programRead{}
	for changed := true; changed; {
		changed = false
		for name, use := range uses {
			total := reads[name]
			for _, method := range use.methods {
				total |= programReadsByMethod[method]
			}
			for _, callee := range use.callees {
				total |= reads[callee]
			}
			if total != reads[name] {
				reads[name] = total
				changed = true
			}
		}
	}
	for name := range uses {
		if _, present := reads[name]; !present {
			reads[name] = 0
		}
	}
	return reads
}

// contextFieldsByDirectory is, for each directory of paths, the name of every struct field of type
// rule.Context its files declare. A directory is a package, so a walker's field declared beside its
// rule is the context in every file of the package that reads it.
func contextFieldsByDirectory(test *testing.T, paths []string) map[string]map[string]bool {
	test.Helper()

	fields := map[string]map[string]bool{}
	for _, path := range paths {
		file, parseFileError := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseFileError != nil {
			test.Fatalf("parsing %s: %v", path, parseFileError)
		}
		directory := filepath.Dir(path)
		ast.Inspect(file, func(node ast.Node) bool {
			structType, isStruct := node.(*ast.StructType)
			if !isStruct {
				return true
			}
			for _, field := range structType.Fields.List {
				if !isRuleSelector(field.Type, "Context") {
					continue
				}
				if fields[directory] == nil {
					fields[directory] = map[string]bool{}
				}
				for _, name := range field.Names {
					fields[directory][name.Name] = true
				}
			}
			return true
		})
	}
	return fields
}

// readProgramFacts parses one file and reports what it does with the program.
//
// A context is known by its type: the names of the file's rule.Context parameters, and contextFields,
// the rule.Context fields its package declares in any file, so `walker.ruleContext.Program` in a helper
// file is caught as a parameter's is. ctx and context are added for the rare function literal whose
// parameter types are elided.
func readProgramFacts(
	test *testing.T,
	path string,
	helpers map[string]programRead,
	contextFields map[string]bool,
) programFacts {
	test.Helper()

	file, parseFileError := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if parseFileError != nil {
		test.Fatalf("parsing %s: %v", path, parseFileError)
	}

	contextNames := map[string]bool{"ctx": true, "context": true}
	for name := range contextFields {
		contextNames[name] = true
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if function, isFunction := node.(*ast.FuncType); isFunction && function.Params != nil {
			for _, field := range function.Params.List {
				if isRuleSelector(field.Type, "Context") {
					for _, name := range field.Names {
						contextNames[name.Name] = true
					}
				}
			}
		}
		return true
	})

	// isContextProgram is `<context>.Program`, where the context is a name or the last link of a
	// chain (`state.ctx.Program`).
	isContextProgram := func(expression ast.Expr) bool {
		selector, isSelector := expression.(*ast.SelectorExpr)
		if !isSelector || selector.Sel.Name != "Program" {
			return false
		}
		switch receiver := selector.X.(type) {
		case *ast.Ident:
			return contextNames[receiver.Name]
		case *ast.SelectorExpr:
			return contextNames[receiver.Sel.Name]
		}
		return false
	}

	facts := programFacts{}
	// The function a rule names as its ProgramFingerprint, which the walk calls with the rule's own view of
	// the program, so what it reads is the rule's to declare as much as anything Run reads.
	var fingerprint string
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			if isContextProgram(typed) {
				facts.readsProgram = true
			}
		case *ast.KeyValueExpr:
			if key, isIdentifier := typed.Key.(*ast.Ident); isIdentifier && key.Name == "ProgramFingerprint" {
				if function, isNamed := typed.Value.(*ast.Ident); isNamed {
					fingerprint = function.Name
				} else {
					facts.unseenCallees = append(
						facts.unseenCallees,
						"a ProgramFingerprint that is not a named function",
					)
				}
				facts.readsProgram = true
			}
			if key, isIdentifier := typed.Key.(*ast.Ident); isIdentifier && key.Name == "ProgramReads" {
				ast.Inspect(typed.Value, func(inner ast.Node) bool {
					if selector, isSelector := inner.(*ast.SelectorExpr); isSelector {
						facts.declared |= programReadsByConstant[selector.Sel.Name]
					}
					return true
				})
			}
		case *ast.CompositeLit:
			if selector, isRule := ruleLiteralSelector(typed.Type); isRule && isRuleSelector(selector, "Rule") {
				facts.definesRule = true
			}
		}
		return true
	})

	if fingerprint != "" {
		read, known := helpers[file.Name.Name+"."+fingerprint]
		if !known {
			facts.unseenCallees = append(facts.unseenCallees, file.Name.Name+"."+fingerprint)
		}
		facts.reads |= read
	}

	uses := usesOf(file, file.Name.Name, isContextProgram)
	for _, method := range uses.methods {
		read, known := programReadsByMethod[method]
		if !known {
			facts.unseenCallees = append(facts.unseenCallees, "Program."+method)
			continue
		}
		facts.reads |= read
	}
	for _, callee := range uses.callees {
		read, known := helpers[callee]
		if !known {
			facts.unseenCallees = append(facts.unseenCallees, callee)
			continue
		}
		facts.reads |= read
	}
	return facts
}

// isRuleSelector reports whether an expression is `rule.<name>`.
func isRuleSelector(expression ast.Expr, name string) bool {
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	packageName, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && packageName.Name == "rule" && selector.Sel.Name == name
}

// ruleLiteralSelector is the `<qualifier>.Rule` a composite literal's type names, seen through the options type
// argument: a Rule is generic over its options, so a rule's literal is `rule.Rule[Options]{...}`, and a scan that
// matched only the bare selector would find no rule at all and pass on nothing.
func ruleLiteralSelector(literalType ast.Expr) (*ast.SelectorExpr, bool) {
	named := literalType
	if indexed, isIndexed := literalType.(*ast.IndexExpr); isIndexed {
		named = indexed.X
	}
	selector, isSelector := named.(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != "Rule" {
		return nil, false
	}
	return selector, true
}

// ruleSourceFiles lists the non-test Go files under internal/lint/rules.
func ruleSourceFiles(test *testing.T) []string {
	test.Helper()
	return goSourceFilesUnder(test, "../lint/rules")
}

// ruleInterfaceSourceFiles lists the non-test Go files of the rule package, whose helpers taking a
// Program (rule.FingerprintPath) read it for the rules that call them.
func ruleInterfaceSourceFiles(test *testing.T) []string {
	test.Helper()
	return goSourceFilesUnder(test, "../lint/rule")
}

// checkingSourceFiles lists the non-test Go files of the shared type-checking helpers, which take a
// rule.Program from the rules that call them.
func checkingSourceFiles(test *testing.T) []string {
	test.Helper()
	return goSourceFilesUnder(test, "../lint/checking")
}

func goSourceFilesUnder(test *testing.T, root string) []string {
	test.Helper()

	var found []string
	walkError := filepath.Walk(root, func(path string, info os.FileInfo, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		found = append(found, path)
		return nil
	})
	if walkError != nil {
		test.Fatalf("walking %s: %v", root, walkError)
	}
	return found
}
