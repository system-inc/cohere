package registry

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/docs_data/capture"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// optionsReadUnreachableOnProbe names the rules whose options read cannot be reached on the probe
// source, each with why. Empty when it landed: all 172 exercised rules read their options at the top
// of Run. A rule added here is one the agreement check does not cover, so it needs its own decoder
// test that drives a real fixture through its registration's Decode.
//
// The fifteen React Compiler rules decode only `{}`, which ESLint writes into resolved configs and
// which carries nothing, so their Run has no option to read (#d21war2). A non-empty object is
// refused at decode; TestCompilerRulesAcceptOnlyTheEmptyObject drives each one's registered Decode.
//
// react-hooks/rules-of-hooks accepts `additionalHooks` and reads it nowhere, because upstream's rule
// reads it nowhere either; TestRulesOfHooksAdditionalHooksChangesNothing pins that.
var optionsReadUnreachableOnProbe = map[string]bool{
	"react-hooks/rules-of-hooks":               true,
	"react-hooks/config":                       true,
	"react-hooks/error-boundaries":             true,
	"react-hooks/globals":                      true,
	"react-hooks/immutability":                 true,
	"react-hooks/incompatible-library":         true,
	"react-hooks/no-deriving-state-in-effects": true,
	"react-hooks/preserve-manual-memoization":  true,
	"react-hooks/purity":                       true,
	"react-hooks/refs":                         true,
	"react-hooks/set-state-in-effect":          true,
	"react-hooks/set-state-in-render":          true,
	"react-hooks/static-components":            true,
	"react-hooks/unsupported-syntax":           true,
	"react-hooks/use-memo":                     true,
	"react-hooks/void-use-memo":                true,
}

// Every registration's decoder hands its rule options the rule reads.
//
// Run used to take `options any`, so nothing at compile time compared the type a Decode returned with
// the type its rule asserted. `unified-signatures` asserted a pointer over the value its decoder
// returned, every configured option was ignored, and every fixture stayed green because the fixtures
// handed Run options directly instead of decoding them (#qmvkf83). A Registration now pairs a rule and
// its decoder over one Options type, so that disagreement is a compile error (#ymag4q6). What is left
// to check at run time is that the decoded options reach the rule at all: each decoder is fed the
// smallest option that means "configured" (an empty object, or an empty option list), and the rule
// bound to whatever it decodes is run, as the walk would run it.
//
// A decoder that refuses the empty input (a required key, a strict schema) is skipped and counted,
// and the count has a floor, so a harness that silently decoded nothing cannot pass.
//
// Running a rule is not reaching its options read: a rule that reads its options only inside a listener
// for a node kind the probe source lacks would run, pass, and check nothing (#zwd43jn). So each rule
// must also move rule.OptionsReads, unless it is named in optionsReadUnreachableOnProbe. Measured
// when this landed: 172 of 172 reach it.
// Not parallel: it reads the process-wide counter rule.OptionsReads before and after each rule's run to
// tell whether the run reached its options, so a parallel test reading options at the same moment would
// make an unreached rule look reached.
func TestEveryDecoderHandsItsRuleTheOptionsTheRuleReads(test *testing.T) {
	exercised := 0
	refused := 0
	var unreached []string
	for _, registration := range rule.Registered() {
		bound, decodeError, decodes := bindEmptyOption(registration)
		if !decodes {
			continue
		}
		if decodeError != nil {
			refused++
			continue
		}
		exercised++
		readsBefore := rule.OptionsReads.Load()
		// Not parallel: its parent reads rule.OptionsReads right after this subtest returns, so the run
		// must have finished by then.
		test.Run(registration.Name, func(test *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				// A panic is the crash corpus's business, not this test's.
				test.Logf("%s panicked on the probe: %v", registration.Name, recovered)
			}()
			if bound.NeedsTypeChecker {
				rule_testing.RunTypedBound(test, bound, "/repository/source/Probe.tsx", "export const value = 1;\n")
				return
			}
			rule_testing.RunBound(test, bound, "/repository/source/Probe.tsx", "export const value = 1;\n")
		})
		if rule.OptionsReads.Load() == readsBefore && !optionsReadUnreachableOnProbe[registration.Name] {
			unreached = append(unreached, registration.Name)
		}
	}
	if len(unreached) > 0 {
		test.Errorf("%d rules ran with decoded options without reading them on the probe source, "+
			"so the agreement check never looked at them: %v. Read options at the top of Run, or name the "+
			"rule in optionsReadUnreachableOnProbe with why", len(unreached), unreached)
	}
	registered := map[string]bool{}
	for _, registration := range rule.Registered() {
		registered[registration.Name] = true
	}
	for name := range optionsReadUnreachableOnProbe {
		if !registered[name] {
			test.Errorf("optionsReadUnreachableOnProbe names %s, which is not registered", name)
		}
	}
	// About 110 registrations carry a decoder today. Under 80 exercised means the empty input stopped
	// decoding for most of them, or the walk is reading the wrong list, rather than that the tree
	// lost its options.
	if exercised < 80 {
		test.Fatalf("only %d decoders produced options to hand their rule (%d refused the empty input), "+
			"so this check proved little", exercised, refused)
	}
}

// bindEmptyOption binds a registration's rule to its decoder's reading of the smallest configured input it
// takes. decodes is false for a rule that takes no options, or whose decoder needs to know where the config
// lives.
func bindEmptyOption(registration rule.Entry) (bound rule.Bound, decodeError error, decodes bool) {
	switch registration.Arity {
	case rule.OptionArityOne:
		bound, decodeError = registration.Bind([]byte(`{}`), rule.OptionsBase{})
		return bound, decodeError, true
	case rule.OptionArityList:
		bound, decodeError = registration.Bind([]byte(`[{}]`), rule.OptionsBase{})
		if decodeError != nil {
			bound, decodeError = registration.Bind([]byte(`[]`), rule.OptionsBase{})
		}
		return bound, decodeError, true
	case rule.OptionArityNone, rule.OptionArityAt:
	}
	return rule.Bound{}, nil, false
}

type optionsReachControlOptions struct {
	Enabled bool
}

// optionsReachControl reads its options only inside a class listener, the shape the reach check
// exists to catch.
var optionsReachControl = rule.Rule[optionsReachControlOptions]{
	Name: "control/options-read-inside-a-listener",
	Run: func(ruleContext rule.Context, options rule.Given[optionsReachControlOptions]) rule.Listeners {
		return rule.Listeners{ast.KindClassDeclaration: func(node *ast.Node) {
			options.Read()
		}}
	},
}

// The reach counter can fail: a rule reading its options only in a class listener does not move it
// on the probe source, and does on a class. Without this the reach assertion above could be passing
// because the counter moves on every run.
// Not parallel: it asserts the process-wide counter rule.OptionsReads does not move across a run, which
// a parallel test reading options would break.
func TestTheOptionsReachCounterSeesARuleThatNeverReadsItsOptions(test *testing.T) {
	before := rule.OptionsReads.Load()
	rule_testing.RunWithOptions(
		test,
		optionsReachControl,
		"/repository/source/Probe.tsx",
		"export const value = 1;\n",
		optionsReachControlOptions{},
	)
	if rule.OptionsReads.Load() != before {
		test.Fatal(
			"the control moved the counter on a source with no class, so the counter cannot tell reached from run",
		)
	}
	rule_testing.RunWithOptions(
		test,
		optionsReachControl,
		"/repository/source/Probe.tsx",
		"export class Probe {}\n",
		optionsReachControlOptions{},
	)
	if rule.OptionsReads.Load() == before {
		test.Fatal("the control did not move the counter on a class, so the counter is not counting")
	}
}

// optionsReachUnderCaptureChild marks the child process TestTheOptionsReachCounterHoldsUnderDocsCapture starts.
// Unset, TestOptionsReachUnderCaptureHelper skips, so neither an ordinary run nor the docs capture's own run of
// this package records the control rule.
const optionsReachUnderCaptureChild = "COHERE_OPTIONS_REACH_UNDER_CAPTURE_CHILD"

// TestOptionsReachUnderCaptureHelper records one asserted case of the control in the child process, so the
// parent can tell from the capture directory that capture was on there. The counter test records nothing,
// since it asserts through the counter rather than an Expect, so its passing alone would not show it.
// Not parallel: in the child it runs beside the counter test, which asserts the process-wide counter does not
// move, so a counted read here at the same moment would fail that test for this helper's reason.
func TestOptionsReachUnderCaptureHelper(test *testing.T) {
	if os.Getenv(optionsReachUnderCaptureChild) == "" {
		test.Skip("run by TestTheOptionsReachCounterHoldsUnderDocsCapture")
	}
	rule_testing.ExpectClean(test, rule_testing.RunWithOptions(
		test,
		optionsReachControl,
		"/repository/source/Probe.tsx",
		"export const value = 1;\n",
		optionsReachControlOptions{},
	))
}

// The reach counter test passes with the docs capture on. The capture records the options each case ran
// with, and when it read them through the counted Read the control moved the counter on a source with no
// class, so regenerating the docs with -capture failed this package. capture.Directory reads the environment
// once per process, so capture cannot be switched on inside this one: the test binary is run again as a
// child with COHERE_DOCS_CAPTURE set, running only the counter test and the helper that proves capture on.
func TestTheOptionsReachCounterHoldsUnderDocsCapture(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	child := exec.Command(
		os.Args[0],
		"-test.run=^(TestTheOptionsReachCounterSeesARuleThatNeverReadsItsOptions|TestOptionsReachUnderCaptureHelper)$",
		"-test.v",
	)
	child.Env = append(os.Environ(), capture.Variable+"="+directory, optionsReachUnderCaptureChild+"=1")
	output, runError := child.CombinedOutput()
	if runError != nil {
		test.Fatalf("the counter test failed with the docs capture on: %v\n%s", runError, output)
	}
	// Exit 0 also follows a pattern that matched nothing, so the counter test must be seen to have passed.
	if !strings.Contains(string(output), "--- PASS: TestTheOptionsReachCounterSeesARuleThatNeverReadsItsOptions") {
		test.Fatalf("the child never ran the counter test:\n%s", output)
	}

	paths, globError := filepath.Glob(filepath.Join(directory, "*"))
	if globError != nil {
		test.Fatal(globError)
	}
	recorded := 0
	for _, path := range paths {
		file, openError := os.Open(path)
		if openError != nil {
			test.Fatal(openError)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var record capture.Record
			if decodeError := json.Unmarshal(scanner.Bytes(), &record); decodeError != nil {
				test.Fatalf("%s: %v", path, decodeError)
			}
			if record.Rule == optionsReachControl.Name {
				recorded++
			}
		}
		if scanError := scanner.Err(); scanError != nil {
			test.Fatalf("%s: %v", path, scanError)
		}
		if closeError := file.Close(); closeError != nil {
			test.Fatalf("%s: %v", path, closeError)
		}
	}
	// Without a record the child ran with capture off, and its passing says nothing about capture.
	if recorded == 0 {
		test.Fatalf("the child recorded no case of the control in %s, so capture was not on there:\n%s",
			directory, output)
	}
}
