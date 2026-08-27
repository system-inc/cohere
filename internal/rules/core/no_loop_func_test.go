package core

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// loopFuncFile is where the fixtures pretend to live.
const loopFuncFile = "/repository/source/LoopFunctions.ts"

// harnessSource is the fixture as the harness actually writes it to disk.
//
// `ruletest.RunTyped` writes `strings.TrimSpace(contents)+"\n"`, so a case copied from an upstream
// tester with a leading newline is one byte offset from the Go literal. Slicing the literal to
// assert a span would report a finding correctly anchored on a function as though it began one
// character early. 34 of these 96 cases are multiline and start with a newline, so this is not
// hypothetical here.
func harnessSource(source string) string {
	return strings.TrimSpace(source) + "\n"
}

// TestNoLoopFuncFires runs every corpus input upstream reports on.
//
// The assertion is on the rendered message rather than only the id, because the message names the
// unsafe variables and their order, and that list is the rule's actual output. A rule finding the
// right function while naming the wrong variable passes an id fixture.
func TestNoLoopFuncFires(t *testing.T) {
	for _, testCase := range loopFuncFiringCases {
		t.Run(testCase.source, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			ruletest.ExpectFindings(t, result, "unsafeRefs")

			// Upstream renders "...unsafe references to variable(s) 'i', 'j'." Ours says why first
			// and names the same list in the same order, so the assertion is on that list, spelled
			// as a literal here rather than taken from the rule's own constant.
			wantSuffix := "The unsafe reference is to " + testCase.unsafeVariables + "."
			got := result.Diagnostics[0].Message.Description
			if !strings.HasSuffix(got, wantSuffix) {
				t.Errorf("message: got %q, want it to end with %q", got, wantSuffix)
			}
		})
	}
}

// TestNoLoopFuncStaysSilent runs every corpus input upstream leaves alone.
//
// This half carries the discrimination. A rule that reports any function inside a loop passes most
// of the firing table and fails here on every `let` case, every `const` case, every undeclared
// name, every immediately invoked function, and every variable nothing ever writes.
func TestNoLoopFuncStaysSilent(t *testing.T) {
	for _, testCase := range loopFuncCleanCases {
		t.Run(testCase.source, func(t *testing.T) {
			ruletest.ExpectClean(t,
				ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source))
		})
	}
}

// TestNoLoopFuncSpans asserts where the finding points.
//
// Upstream reports on the function node, so the span runs from the function's first token to its
// last. No message-id fixture can see this, and the rule carries no fix, so this is the only thing
// that pins it.
func TestNoLoopFuncSpans(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			"a function expression, not the parentheses around it",
			"for (var i=0; i<l; i++) { (function() { i; }) }",
			"function() { i; }",
		},
		{
			"an arrow function, whole",
			"for (var i=0; i < l; i++) { (() => { i; }) }",
			"() => { i; }",
		},
		{
			"a function declaration, from its keyword",
			"for (var i=0; i<l; i++) { function foo() { i; } }",
			"function foo() { i; }",
		},
		{
			"the inner function of a nested pair, which is the one reported",
			"for (var i=0; i<l; i++) { for (var j=0; j<m; j++) { (function() { i+j; }) } }",
			"function() { i+j; }",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			ruletest.ExpectFindings(t, result, "unsafeRefs")
			// Sliced out of the source the harness wrote, not out of the Go literal.
			onDisk := harnessSource(testCase.source)
			got := onDisk[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
			if got != testCase.want {
				t.Errorf("span: got %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestNoLoopFuncMessage asserts the message id and the fixed half of the description.
//
// The description interpolates, so the id assertion cannot see anything the format string does.
// Asserted against literals typed here rather than against the rule's own constant, because
// comparing a diagnostic to the constant it was built from is an equality both sides of which move
// together under mutation.
func TestNoLoopFuncMessage(t *testing.T) {
	result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, "for (var i=0; i<l; i++) { (function() { i; }) }")
	ruletest.ExpectFindings(t, result, "unsafeRefs")
	message := result.Diagnostics[0].Message

	if message.Id != "unsafeRefs" {
		t.Errorf("message id: got %q, want %q", message.Id, "unsafeRefs")
	}
	const wantPrefix = "This function is created inside a loop and closes over a variable the loop reassigns,"
	if !strings.HasPrefix(message.Description, wantPrefix) {
		t.Errorf("description: got %q, want it to start with %q", message.Description, wantPrefix)
	}
	const wantSuffix = "The unsafe reference is to 'i'."
	if !strings.HasSuffix(message.Description, wantSuffix) {
		t.Errorf("description: got %q, want it to end with %q", message.Description, wantSuffix)
	}
	// The interpolated tail must not be doubled or mangled, which a prefix and a suffix check
	// together cannot see on their own. One occurrence of the variable name is the property meant.
	if strings.Count(message.Description, "'i'") != 1 {
		t.Errorf("description names 'i' %d times, want 1: %q",
			strings.Count(message.Description, "'i'"), message.Description)
	}
}

// TestNoLoopFuncRequiresTheTypedHarness pins that the rule declines without a checker.
//
// Measured: `GetSymbolAtLocation` on a nil checker returns nil rather than crashing, so a rule
// missing its guard goes silently inert instead of announcing itself. Every StaysSilent case would
// then pass vacuously. This asserts the plain harness produces nothing on an input the typed
// harness reports, so a later revert of the guard fails loudly rather than going quiet.
func TestNoLoopFuncRequiresTheTypedHarness(t *testing.T) {
	const source = "for (var i=0; i<l; i++) { (function() { i; }) }"

	ruletest.ExpectFindings(t,
		ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, source), "unsafeRefs")

	ruletest.ExpectClean(t,
		ruletest.Run(t, NoLoopFunc, loopFuncFile, source))

	if !NoLoopFunc.NeedsTypeChecker {
		t.Error("the rule must declare NeedsTypeChecker, or the live run hands it a nil checker")
	}
}

// TestNoLoopFuncBorderIsTheOutermostLoop covers the discrimination the corpus states only once.
//
// The border question only arises for a binding that is not already answered by an earlier arm, so
// the obvious probe does not test it: `for (let i=0; ...)` short circuits on "a `let` declared
// inside the loop is a fresh binding", and the border is never consulted. My first fixture for this
// was that shape, asserted as firing, and the installed build says it is clean.
//
// The input that actually separates the two readings is a binding declared OUTSIDE both loops with
// a write positioned BETWEEN them. That write is after the outer loop's start and before the inner
// loop's start, so taking the innermost containing loop as the border calls it safe and taking the
// outermost calls it unsafe. Upstream takes the outermost, and an outer iteration really can run
// that write and then re-enter the inner loop.
//
// Measured against the installed eslint 10.8.1 build, every row.
func TestNoLoopFuncBorderIsTheOutermostLoop(t *testing.T) {
	cases := []struct {
		name   string
		source string
		fires  bool
	}{
		{
			"a write between the two loops is after the outermost loop's start",
			"let i; for (var a=0;a<l;a++) { i = 7; for (var b=0;b<m;b++) { (function(){ i; }) } }",
			true,
		},
		{
			"the same shape with no write anywhere is clean",
			"let i; for (var a=0;a<l;a++) { for (var b=0;b<m;b++) { (function(){ i; }) } }",
			false,
		},
		{
			"a write after both loops is unsafe too, being past the border in the other direction",
			"var i; for (var a=0;a<l;a++) { for (var b=0;b<m;b++) { (function(){ i; }) } i = 7; }",
			true,
		},
		{
			"a write entirely BEFORE the outermost loop is safe, which is what the border means",
			"let i = 0; i = 1; for (var a=0;a<l;a++) { (function(){ i; }) }",
			false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			if testCase.fires {
				ruletest.ExpectFindings(t, result, "unsafeRefs")
			} else {
				ruletest.ExpectClean(t, result)
			}
		})
	}
}

// TestNoLoopFuncLetDeclarationStopsTheBorderClimb covers the second argument to the border.
//
// `getTopLoopNode` is given the declaration to stop at when the binding is a `let`, so the climb
// out to the outermost loop halts at the `let` rather than passing it. The corpus never separates
// the two, and a mutant that ignored the argument survived all 96 cases plus every other invented
// one here.
//
// The distinguishing shape needs three things at once: a `let` declared inside an OUTER loop, the
// function inside an INNER loop, and a write positioned between the `let` and the inner loop. With
// the stop, the border is the `let`'s end and the write is before nothing that matters, so it is
// safe. Without it, the border is the outer loop's start, the write sits after it, and the rule
// reports.
//
// Measured against the installed eslint 10.8.1 build, every row.
func TestNoLoopFuncLetDeclarationStopsTheBorderClimb(t *testing.T) {
	cases := []struct {
		name   string
		source string
		fires  bool
	}{
		{
			"a write after the let but before the inner loop is safe",
			"for (var a=0;a<l;a++) { let x; x = 1; for (var b=0;b<m;b++) { (function(){ x; }) } }",
			false,
		},
		{
			"a write AFTER the inner loop is not, being past the border either way",
			"for (var a=0;a<l;a++) { let x = 0; for (var b=0;b<m;b++) { (function(){ x; }) } x = 1; }",
			true,
		},
		{
			"with no write at all the same shape is clean",
			"for (var a=0;a<l;a++) { let x = 0; for (var b=0;b<m;b++) { (function(){ x; }) } }",
			false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			if testCase.fires {
				ruletest.ExpectFindings(t, result, "unsafeRefs")
			} else {
				ruletest.ExpectClean(t, result)
			}
		})
	}
}

// TestNoLoopFuncHeadPositionsThatAreOutsideTheLoop pins which parts of a loop head run once.
//
// The corpus carries clean cases for the `for` initializer and the iterated expression, and neither
// is evidence about this. `for (var x in xs.filter(function(x) { return x != upper; })) { }` passes
// because `upper` is UNDECLARED, so the rule declines it before position is consulted at all;
// removing the `filter` leaves it passing. A mutant treating the iterated expression as inside the
// loop survived the whole corpus.
//
// Separating the two needs a binding that is genuinely unsafe, which means a write positioned after
// the loop. Then a function in the iterated expression is clean and the same function in the body
// reports, and the only difference is position.
//
// The `for` test is the other half and it goes the other way: upstream returns the loop for it, so
// a function there IS in the loop, and only the initializer is outside.
//
// Measured against the installed eslint 10.8.1 build, every row.
func TestNoLoopFuncHeadPositionsThatAreOutsideTheLoop(t *testing.T) {
	cases := []struct {
		name   string
		source string
		fires  bool
	}{
		{
			"a function in a for-in iterated expression runs once, so it is outside",
			"var u; for (var x in xs.filter(function() { return u; })) { } u = 1;",
			false,
		},
		{
			"the same function in the body is inside",
			"var u; for (var x in {}) { (function() { u; }) } u = 1;",
			true,
		},
		{
			"a function in a for-of iterated expression is outside too",
			"var u; for (var x of xs.filter(function() { return u; })) { } u = 1;",
			false,
		},
		{
			"the same function in the body is inside",
			"var u; for (var x of {}) { (function() { u; }) } u = 1;",
			true,
		},
		{
			"a function in a for initializer runs once, so it is outside",
			"var u; for (var i=0, a=function() { u; }; i<l; i++) { } u = 1;",
			false,
		},
		{
			"a function in the for TEST is inside, which the initializer case does not imply",
			"var u; for (var i=0; (function() { u; }), i<l; i++) { } u = 1;",
			true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			if testCase.fires {
				ruletest.ExpectFindings(t, result, "unsafeRefs")
			} else {
				ruletest.ExpectClean(t, result)
			}
		})
	}
}

// TestNoLoopFuncPropertyNamesAreNotReferences pins a behaviour, not a branch.
//
// A name in the property position of a member access, or the key of an object literal, must not be
// read as a reference to a like-named variable, and these rows assert that against the installed
// eslint 10.8.1 build. What they do NOT do is exercise `loopFuncIsReadableReference`: a mutant
// neutralising that filter survives all of them, and the doc comment on that function records why.
// Resolution already separates these positions, so the filter cannot change a verdict and no
// fixture can make it.
//
// Kept because the behaviour is worth pinning wherever it comes from. If the checker ever resolved
// a property name to the variable, these are the rows that would go red, and at that point the
// filter would stop being equivalent and start being the thing that saves it.
//
// The fourth row runs the other direction: a function reading BOTH the property and the variable
// still reports, and names the variable once rather than twice.
//
// Measured against the installed eslint 10.8.1 build, every row.
func TestNoLoopFuncPropertyNamesAreNotReferences(t *testing.T) {
	cases := []struct {
		name   string
		source string
		fires  bool
	}{
		{
			"a property named like an unsafe variable is not a reference to it",
			"var u; for (var i=0;i<3;i++) { (function(){ o.u; }) } u = 1;",
			false,
		},
		{
			"an object literal key is not either",
			"var u; for (var i=0;i<3;i++) { (function(){ ({u: 1}); }) } u = 1;",
			false,
		},
		{
			"the bare name is",
			"var u; for (var i=0;i<3;i++) { (function(){ u; }) } u = 1;",
			true,
		},
		{
			"reading both still reports, and names the variable once",
			"var u; for (var i=0;i<3;i++) { (function(){ o.u; u; }) } u = 1;",
			true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			if !testCase.fires {
				ruletest.ExpectClean(t, result)
				return
			}
			ruletest.ExpectFindings(t, result, "unsafeRefs")
			const want = "The unsafe reference is to 'u'."
			got := result.Diagnostics[0].Message.Description
			if !strings.HasSuffix(got, want) {
				t.Errorf("message: got %q, want it to end with %q", got, want)
			}
		})
	}
}

// TestNoLoopFuncMergedVariableDeclarations covers a symbol carrying more than one declaration.
//
// `var u; ... var u = 1;` is one binding with two declaration nodes, and the checker hands both back
// on one symbol. The corpus writes no redeclaration at all, so this shape is unexercised by it and
// these rows are the only thing asserting the rule handles it.
//
// They do NOT pin the earliest-versus-latest choice in `loopFuncDeclarationFor`: a mutant taking
// the latest survives them, and that function's doc comment records the measurement saying why it
// must. The choice is unobservable because it is used as an index key everywhere except one arm,
// and that arm only ever sees block-scoped bindings, which cannot be redeclared.
//
// The first two rows are still worth having: identical except for which declaration carries the
// initializer, and upstream answers them differently, so they pin that the WRITE is found on
// whichever declaration carries it rather than only on a canonical one.
//
// Measured against the installed eslint 10.8.1 build, every row.
func TestNoLoopFuncMergedVariableDeclarations(t *testing.T) {
	cases := []struct {
		name   string
		source string
		fires  bool
	}{
		{
			"the initializer on the later declaration is a write after the loop",
			"var u; for (var a=0;a<3;a++) { (function(){ u; }) } var u = 1;",
			true,
		},
		{
			"the same initializer on the earlier declaration is before the loop, so it is safe",
			"var u = 1; for (var a=0;a<3;a++) { (function(){ u; }) } var u;",
			false,
		},
		{
			"two initialized declarations after the loop still report once",
			"for (var a=0;a<3;a++) { (function(){ u; }) } var u = 1; var u = 2;",
			true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			if testCase.fires {
				ruletest.ExpectFindings(t, result, "unsafeRefs")
			} else {
				ruletest.ExpectClean(t, result)
			}
		})
	}
}

// TestNoLoopFuncLetStatementRangeIncludesItsSemicolon pins which node the `let` comparisons use.
//
// `loopFuncDeclarationStatement` lifts from the declaration list to the statement that wraps it,
// and the two differ by the trailing semicolon at the end and by any modifier at the start
// (`declare let y` puts the statement at 0 and the list at 7). Both ends feed position comparisons.
//
// A mutant that never lifts survives this case, and that function's doc comment records why: for
// the gap to matter a loop boundary would have to fall inside it. What this row does pin is the
// behaviour itself, a `let` declared in a loop body with a write after the inner loop, measured
// against the installed eslint 10.8.1 build.
func TestNoLoopFuncLetStatementRangeIncludesItsSemicolon(t *testing.T) {
	const source = "for (var a=0;a<3;a++) { let x; for (var b=0;b<3;b++) { (function(){ x; }) } x = 1; }"
	ruletest.ExpectFindings(t,
		ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, source), "unsafeRefs")
}

// TestNoLoopFuncDestructuredBindingsCarryTheirKind covers a false positive the corpus cannot see.
//
// `for (const [, channel] of channels)` binds through a BindingElement rather than through a
// VariableDeclaration, so a binding-kind test requiring the latter answers "other" and the constant
// arm never fires. The rule then reports on a `const`, which upstream never does.
//
// This was not found by any fixture. It was found by the dry run over the ahra tree, where it
// produced four findings the installed eslint 10.8.1 build does not make, and the corpus could not
// have caught it: upstream's ESTree already puts a VariableDeclaration above every declarator
// including a destructured one, so the shape does not arise there, and its single destructured loop
// head is a `var`, which lands on "other" either way.
//
// The `var` row is what keeps the repair honest: the climb must read the kind, not assume safety.
//
// Measured against the installed eslint 10.8.1 build, every row.
func TestNoLoopFuncDestructuredBindingsCarryTheirKind(t *testing.T) {
	cases := []struct {
		name   string
		source string
		fires  bool
	}{
		{
			"an array-destructured const is constant, so it is safe",
			"for (const [, c] of xs) { (function(){ c; }) }",
			false,
		},
		{
			"an object-destructured const likewise",
			"for (const {a} of xs) { (function(){ a; }) }",
			false,
		},
		{
			"a destructured let is a fresh binding each iteration, so it is safe too",
			"for (let [, c] of xs) { (function(){ c; }) }",
			false,
		},
		{
			"a destructured var is neither, and still reports",
			"for (var [, c] of xs) { (function(){ c; }) }",
			true,
		},
		{
			"a safe destructured head does not make an unsafe outer variable safe",
			"var u; for (const [, c] of xs) { (function(){ u; }) } u = 1;",
			true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			if testCase.fires {
				ruletest.ExpectFindings(t, result, "unsafeRefs")
			} else {
				ruletest.ExpectClean(t, result)
			}
		})
	}
}

// TestNoLoopFuncAsyncImmediatelyInvokedIsNotSkipped covers the half of the skip the corpus omits.
//
// Upstream applies the immediately-invoked skip only to a function that is neither async nor a
// generator, and its corpus writes no async or generator case at all. The arm is therefore
// reachable and untested by any imported fixture, which is exactly the shape that ships.
//
// Measured against the installed eslint 10.8.1 build: the plain arrow is clean and the async one
// reports, so the condition is load-bearing rather than defensive.
func TestNoLoopFuncAsyncImmediatelyInvokedIsNotSkipped(t *testing.T) {
	cases := []struct {
		name   string
		source string
		fires  bool
	}{
		{
			"a plain immediately invoked arrow is skipped",
			"for (var i = 0; i < 10; ++i) { (()=>{ i;})() }",
			false,
		},
		{
			"an async one is not, because it outlives the call that made it",
			"for (var i = 0; i < 10; ++i) { (async ()=>{ i;})() }",
			true,
		},
		{
			"a generator function expression is not either",
			"for (var i = 0; i < 10; ++i) { (function*(){ i;})() }",
			true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			if testCase.fires {
				ruletest.ExpectFindings(t, result, "unsafeRefs")
			} else {
				ruletest.ExpectClean(t, result)
			}
		})
	}
}

// TestNoLoopFuncNamedImmediatelyInvokedReachableByName covers the other half of the skip.
//
// A named function expression that is called where it is made is still skipped, unless its own name
// is among the references escaping it, because a reachable name means something can hold onto the
// function past the iteration. Upstream's corpus has the skipped row and not the other one.
//
// Measured against the installed eslint 10.8.1 build, both rows.
func TestNoLoopFuncNamedImmediatelyInvokedReachableByName(t *testing.T) {
	cases := []struct {
		name   string
		source string
		fires  bool
	}{
		{
			"a named one whose name nothing reaches is skipped",
			"for (var i = 0; i < 10; ++i) { (function a(){i;})() }",
			false,
		},
		{
			"one that recurses by its own name is not",
			"for (var i = 0; i < 10; ++i) { (function a(){ i; a; })() }",
			true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunTyped(t, NoLoopFunc, loopFuncFile, testCase.source)
			if testCase.fires {
				ruletest.ExpectFindings(t, result, "unsafeRefs")
			} else {
				ruletest.ExpectClean(t, result)
			}
		})
	}
}
