package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestNoEmptyReportsEmptyBlocks is the fixture that must fire.
func TestNoEmptyReportsEmptyBlocks(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		source string
		wantId string
	}{
		{"if (foo) {}", "unexpectedBlock"},
		{"if (foo) { bar(); } else {}", "unexpectedBlock"},
		{"while (foo) {}", "unexpectedBlock"},
		{"do {} while (foo);", "unexpectedBlock"},
		{"for (;;) {}", "unexpectedBlock"},
		{"for (const key in obj) {}", "unexpectedBlock"},
		{"for (const item of list) {}", "unexpectedBlock"},
		{"try { work(); } catch (error) {}", "unexpectedBlock"},
		{"label: {}", "unexpectedBlock"},
		{"switch (foo) {}", "unexpectedSwitch"},
	} {
		result := rule_testing.Run(test, NoEmpty, "empty.ts", testCase.source)
		rule_testing.ExpectFindings(test, result, testCase.wantId)
	}
}

// TestNoEmptyReportsEachEmptyBlockOfATryStatement pins the count.
//
// A try statement has up to three blocks and each is its own claim about what happens there, so an
// empty try, catch, and finally are three findings rather than one.
func TestNoEmptyReportsEachEmptyBlockOfATryStatement(test *testing.T) {
	test.Parallel()

	result := rule_testing.Run(test, NoEmpty, "try.ts", "try { work(); } catch (error) {} finally {}")
	rule_testing.ExpectFindings(test, result, "unexpectedBlock", "unexpectedBlock")

	all := rule_testing.Run(test, NoEmpty, "all.ts", "try {} catch (error) {} finally {}")
	rule_testing.ExpectFindings(test, all, "unexpectedBlock", "unexpectedBlock", "unexpectedBlock")
}

// TestNoEmptyAcceptsAnyCommentAsIntent pins the escape hatch.
//
// A comment is what turns an empty block from ambiguous into deliberate, so both comment forms count
// and both stay silent wherever a block would otherwise report.
func TestNoEmptyAcceptsAnyCommentAsIntent(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"if (foo) { // nothing to do\n}",
		"while (foo) { /* empty */ }",
		"switch (foo) { /* no arms yet */ }",
		"switch (foo) { // no arms yet\n}",
		"try { work(); } catch (error) { // continue regardless of error\n}",
		"try { work(); } finally { /* continue regardless of error */ }",
		"if (foo) {\n\t/* multi\n\t   line */\n}",
		"if (foo) { /* one */ /* two */ }",
	} {
		result := rule_testing.Run(test, NoEmpty, "commented.ts", source)
		rule_testing.ExpectClean(test, result)
	}
}

// TestNoEmptyIsNotFooledByCommentMarkersInStrings pins that a comment marker in the surrounding
// expression does not read as documentation of the empty block.
//
// Each discriminant here carries `//` or `/*` inside a string or a regular expression while the
// block itself is genuinely empty and genuinely undocumented, so each has to report. This currently
// holds for a second reason as well, which is worth knowing rather than relying on: the CaseBlock
// node spans only its own braces, so a substring search would not see the discriminant either. The
// test pins the behavior at the surface where a reader checks it, and stays correct whichever way
// the check is implemented underneath.
func TestNoEmptyIsNotFooledByCommentMarkersInStrings(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		`switch (path.replace('/*', '')) {}`,
		`switch (text.split('//')[0]) {}`,
		"switch (/[/*]/.test(input) ? 1 : 2) {}",
		`if (label === '/* empty */') {}`,
	} {
		result := rule_testing.Run(test, NoEmpty, "strings.ts", source)
		if len(result.Diagnostics) != 1 {
			test.Fatalf("expected 1 finding for %q, got %d: %v", source, len(result.Diagnostics), result.MessageIds())
		}
	}
}

// TestNoEmptyExemptsFunctionBodies pins where ESLint draws the line.
//
// An empty function is a real thing to write, and it is the concern of `no-empty-function`. Every
// function-like form is here because each reaches the block through a different parent kind, and a
// check that named only some of them would report on the rest.
func TestNoEmptyExemptsFunctionBodies(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"function noop() {}",
		"const noop = function () {};",
		"const noop = () => {};",
		"const noop = async () => {};",
		"async function noop() {}",
		"function* noop() {}",
		"async function* noop() {}",
		"class C { method() {} }",
		"class C { constructor() {} }",
		"class C { constructor(private readonly value: string) {} }",
		"class C { get value() {} }",
		"class C { set value(next: string) {} }",
		"class C { static method() {} }",
		"const obj = { method() {} };",
		"class C { private handle = () => {}; }",
	} {
		result := rule_testing.Run(test, NoEmpty, "functions.ts", source)
		rule_testing.ExpectClean(test, result)
	}
}

// TestNoEmptyStaysSilentOnBlocksWithContent is the half that catches a rule firing on correct code.
func TestNoEmptyStaysSilentOnBlocksWithContent(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"if (foo) { bar(); }",
		"while (foo) { bar(); }",
		"switch (foo) { case 1: break; }",
		"switch (foo) { default: break; }",
		"try { work(); } catch (error) { report(error); }",
		"{ const value = 1; }",
		"if (foo) { ; }",
		"class C {}",
		"const empty = {};",
		"interface Empty {}",
		"enum Empty {}",
	} {
		result := rule_testing.Run(test, NoEmpty, "content.ts", source)
		rule_testing.ExpectClean(test, result)
	}
}

// TestNoEmptyWithoutOptionsReportsEmptyCatch pins the default.
//
// The option defaults off, so a registry that forgets to wire it must produce the strict rule rather
// than a silent one.
func TestNoEmptyWithoutOptionsReportsEmptyCatch(test *testing.T) {
	test.Parallel()

	result := rule_testing.RunWithOptions(test, NoEmpty, "strict.ts", "try { work(); } catch (error) {}", nil)
	rule_testing.ExpectFindings(test, result, "unexpectedBlock")
}

// TestNoEmptyAllowEmptyCatchExemptsOnlyTheCatch pins the option's exact reach.
//
// It names the catch clause and nothing else, so an empty try or finally in the same statement still
// reports. That boundary is the whole option: a codebase that has decided a bare catch is idiomatic
// has said nothing about an empty finally.
func TestNoEmptyAllowEmptyCatchExemptsOnlyTheCatch(test *testing.T) {
	test.Parallel()

	options := NoEmptyOptions{AllowEmptyCatch: true}

	for _, source := range []string{
		"try { work(); } catch (error) {}",
		"try { work(); } catch {}",
		"try { work(); } catch (error) {} finally { cleanup(); }",
	} {
		result := rule_testing.RunWithOptions(test, NoEmpty, "allowed.ts", source, options)
		rule_testing.ExpectClean(test, result)
	}

	finallyStillReports := rule_testing.RunWithOptions(test, NoEmpty, "finally.ts",
		"try { work(); } catch (error) {} finally {}", options)
	rule_testing.ExpectFindings(test, finallyStillReports, "unexpectedBlock")

	tryStillReports := rule_testing.RunWithOptions(test, NoEmpty, "try.ts",
		"try {} catch (error) {}", options)
	rule_testing.ExpectFindings(test, tryStillReports, "unexpectedBlock")

	otherBlocksStillReport := rule_testing.RunWithOptions(test, NoEmpty, "other.ts", "if (foo) {}", options)
	rule_testing.ExpectFindings(test, otherBlocksStillReport, "unexpectedBlock")
}
