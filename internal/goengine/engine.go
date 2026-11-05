// Package goengine is the Go engine: a hidden mode of the cohere binary, `cohere go-engine --contract 3 --root
// <dir> [switches] [--lint-config <file>] [paths]`, that the front door runs as a child the way it runs
// cohere-swift, and whose stdout is swift/Contract.md's record stream.
//
// The pipeline is the contract's: describe the project, then fix and format, then load and type-check, then
// lint, then the unused report, each phase recorded exactly once whether it ran or not, and a summary whose
// verdict could have failed. A phase that did not run, a file nothing saw and a rule that crashed each make the
// run incomplete, so none of them can ever print green.
package goengine

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules"
	"github.com/system-inc/cohere/internal/goengine/suppression"
)

// The exit codes the contract gives an engine.
const (
	// exitClean is a summary with no findings over a complete run.
	exitClean = 0

	// exitFindings is a summary with findings, or over a run that did not check everything.
	exitFindings = 1

	// exitRefused is a run that never started: a bad flag, a contract mismatch, no Go root, a refused config.
	// Nothing is on stdout, and the reason is one sentence on stderr.
	exitRefused = 2

	// exitBroken is a stream the engine could not finish writing. The contract reads any exit but 0, 1 and 2
	// as a crash, which is what a half-written stream is: the front door says nothing was checked.
	exitBroken = 3
)

// ruleRequireFormatting is the rule a file gofmt would change is reported under, the same leaf as Swift's,
// so one name means one thing whichever engine prints it. It is the engine's own rather than an analyzer's,
// since formatting runs before the load.
const ruleRequireFormatting = rule.Namespace + "consistency-require-formatting"

// notFormattedMessage is the finding's text, Swift's verbatim, so the two engines' format gates read alike.
const notFormattedMessage = "not formatted the house way; a run without --no-fix rewrites it"

// notFormattedMessageId is the finding's messageId, Swift's.
const notFormattedMessageId = "notFormatted"

// engineRuleNames are the rules the engine reports under itself rather than through an analyzer: the format
// finding, and suppression's two.
var engineRuleNames = []string{
	ruleRequireFormatting,
	suppression.RuleNoUnusedIgnore,
	suppression.RuleRequireIgnoreReason,
}

// Main runs the engine over the command line the front door sends, writing records to standardOutput and
// prose to standardError, and returns the exit code.
func Main(arguments []string, standardOutput io.Writer, standardError io.Writer) int {
	return run(arguments, standardOutput, standardError, rules.All())
}

// run is Main with the analyzer rules handed in, so a test can run the engine with a rule planted in place of
// the registry's, such as one that panics.
func run(arguments []string, standardOutput io.Writer, standardError io.Writer, registered []rule.Rule) int {
	request, parseError := parseInvocation(arguments)
	if parseError != nil {
		return refuse(standardError, parseError)
	}
	switch request.mode() {
	case modeVersion:
		return writeVersion(standardOutput, standardError)
	case modeRules, modeRulesEnabled:
		return writeRules(request, registered, standardOutput, standardError)
	}

	// Everything that can refuse is read before the first record, so exit 2 never leaves half a stream.
	ruleSettings, settingsError := loadSettings(request, registered)
	if settingsError != nil {
		return refuse(standardError, settingsError)
	}
	if optionsError := configureRules(ruleSettings, registered); optionsError != nil {
		return refuse(standardError, optionsError)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		return refuse(standardError, describeError)
	}
	engine := newPipeline(request, described, ruleSettings, registered, standardOutput, standardError)
	return engine.run()
}

// knownRuleNames is every rule a run knows: the analyzers', and the engine's own.
func knownRuleNames(registered []rule.Rule) []string {
	names := make([]string, 0, len(registered)+len(engineRuleNames))
	for _, registeredRule := range registered {
		names = append(names, registeredRule.Name)
	}
	return append(names, engineRuleNames...)
}

// refuse writes a refusal's reason as one sentence and returns exit 2.
func refuse(standardError io.Writer, refusal error) int {
	// Ignored: stderr is the only place the reason can go, and exit 2 says the run never started either way.
	_, _ = fmt.Fprintln(standardError, refusalSentence(refusal))
	return exitRefused
}

// refusalSentence is an error chain as one sentence: capitalized, with a trailing period. The chain is already
// one line of clauses joined by colons, each lowercase as house errors are, so only its ends change.
func refusalSentence(refusal error) string {
	sentence := strings.TrimSpace(refusal.Error())
	first, size := utf8.DecodeRuneInString(sentence)
	if size == 0 {
		return "The Go engine refused the run and gave no reason."
	}
	sentence = string(unicode.ToUpper(first)) + sentence[size:]
	if !strings.HasSuffix(sentence, ".") {
		sentence += "."
	}
	return sentence
}
