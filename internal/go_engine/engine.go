// Package go_engine is the Go engine: a hidden mode of the cohere binary, `cohere go-engine --contract 3 --root
// <dir> [switches] [--lint-config <file>] [paths]`, that the front door runs as a child the way it runs
// cohere-swift, and whose stdout is swift/Contract.md's record stream.
//
// The pipeline is the contract's: describe the project, then fix and format, then load and type-check, then
// lint, then the unused report, each phase recorded exactly once whether it ran or not, and a summary whose
// verdict could have failed. A phase that did not run, a file nothing saw and a rule that crashed each make the
// run incomplete, so none of them can ever print green.
package go_engine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/go_engine/format"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
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

// mainHasRun is set by the first Main in this process.
var mainHasRun atomic.Bool

// Main runs the engine over the command line the front door sends, writing records to standardOutput and
// prose to standardError, and returns the exit code. availableMemoryBytes is the memory the front door's policy
// read as available when this process started, 0 when it could not, which the memory limit is sized from. A command
// line that names one of the release's hidden verbs for the standard library's table runs that verb instead (see
// runStandardLibraryVerb), under the same memory limit. standardInput is read only by the editor's save, whose buffer
// it is, and whose standardOutput is that file's text rather than records (pipeline.runStdin).
func Main(
	arguments []string,
	availableMemoryBytes uint64,
	standardInput io.Reader,
	standardOutput io.Writer,
	standardError io.Writer,
) int {
	// A run sets its rule options on the registry's analyzers, which outlive it, so a second run in this process
	// would be judged by the first's options with no error. The front door starts one engine process per Go root
	// and main exits after this call, so a second call is a caller's mistake, refused before anything runs.
	if mainHasRun.Swap(true) {
		return refuse(standardError, errors.New("refusing a second engine run in one process, since the first run's "+
			"rule options are still set on the analyzers; start a new engine process for each project"))
	}
	// Here rather than in run, so a test running the engine in process never changes its own memory limit. The limit
	// the front door's policy set is read before capMemory lowers it, since a late raise never passes it (lateLimit).
	inheritedBytes := debug.SetMemoryLimit(-1)
	memory := engineMemory{
		record:         capMemory(os.LookupEnv, debug.SetMemoryLimit, availableMemoryBytes),
		inheritedBytes: inheritedBytes,
		setMemoryLimit: debug.SetMemoryLimit,
		collecting:     collectorMarking,
	}
	if isStandardLibraryVerb(arguments) {
		generator, generatorError := binaryGenerator()
		if generatorError != nil {
			return refuse(standardError, generatorError)
		}
		return runStandardLibraryVerb(arguments, standardOutput, standardError, rules.All(),
			embeddedStandardLibrary.table, generator)
	}
	// A save writes no file, refused where every write of a source file passes (format.RefuseWrites), so no phase, one
	// added later included, can write over a file the editor holds unsaved. Here rather than in run, as the memory
	// limit is, since the refusal is process-wide, and a test running the engine in process writes files beside it.
	if request, parseError := parseInvocation(arguments); parseError == nil && request.mode() == modeStdin {
		format.RefuseWrites()
	}
	return runOverlapping(arguments, standardInput, standardOutput, standardError, rules.All(), &memory,
		embeddedStandardLibrary, true, nil)
}

// run is Main with the analyzer rules handed in and no standard input, so a test can run the engine with a rule
// planted in place of the registry's, such as one that panics. memory is what Main decided about the memory limit, nil
// in a test, which decides nothing and never raises the limit. It reads the standard library's entries the binary
// carries.
func run(
	arguments []string,
	standardOutput io.Writer,
	standardError io.Writer,
	registered []rule.Rule,
	memory *engineMemory,
) int {
	return runWithStandardLibrary(arguments, standardOutput, standardError, registered, memory,
		embeddedStandardLibrary)
}

// runWithStandardLibrary is run reading builtIn in place of the binary's own standard-library entries, so a test can
// give a run a table and a stamp of its own, or collect the entries a run saves, without a release build.
func runWithStandardLibrary(
	arguments []string,
	standardOutput io.Writer,
	standardError io.Writer,
	registered []rule.Rule,
	memory *engineMemory,
	builtIn builtInStandardLibrary,
) int {
	return runOverlapping(arguments, nil, standardOutput, standardError, registered, memory, builtIn, true, nil)
}

// runOverlapping is runWithStandardLibrary, with the standard input the editor's save reads its buffer from, nil for a
// run that has none, overlaps saying whether a run whose findings cache has no store may lint beside its load, which
// only a test turns off, to hold a run that does to the records of one that does not, and answersInPart the packages,
// by import path, whose entries the run writes with only the answers its questions computed (see check.Caching's
// AnswersInPart), which only a test names.
func runOverlapping(
	arguments []string,
	standardInput io.Reader,
	standardOutput io.Writer,
	standardError io.Writer,
	registered []rule.Rule,
	memory *engineMemory,
	builtIn builtInStandardLibrary,
	overlaps bool,
	answersInPart map[string]bool,
) int {
	request, parseError := parseInvocation(arguments)
	if parseError != nil {
		return refuse(standardError, parseError)
	}
	request.standardLibrary = builtIn
	request.answersInPart = answersInPart
	switch request.mode() {
	case modeVersion:
		return writeVersion(standardOutput, standardError)
	case modeRules, modeRulesEnabled:
		return writeRules(request, registered, standardOutput, standardError)
	case modeStdin:
		if standardInput == nil {
			return refuse(standardError, errors.New("--stdin-filepath reads the file's text from standard input, "+
				"and this run was given none"))
		}
		buffer, readError := io.ReadAll(standardInput)
		if readError != nil {
			return refuse(standardError, fmt.Errorf("reading the text of %s from standard input: %w",
				request.stdinPath, readError))
		}
		request.buffer = buffer
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
	if request.mode() == modeStdin {
		// The save's standard output is the file's text, so the records it would write go nowhere.
		engine := newPipeline(request, described, ruleSettings, registered, io.Discard, standardError, memory)
		return engine.runStdin(standardOutput)
	}
	engine := newPipeline(request, described, ruleSettings, registered, standardOutput, standardError, memory)
	engine.overlaps = overlaps
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
