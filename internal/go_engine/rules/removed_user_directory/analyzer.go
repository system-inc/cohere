// Package removed_user_directory is staticcheck's SA9007, a call to os.RemoveAll that deletes the whole of a
// directory the system or the user keeps, the temporary directory, the user's cache, config or home directory, where
// a subdirectory of it was meant, ported onto the engine's own intermediate representation (#tf4cb8g).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa9007/sa9007.go, whose logic it keeps and
// whose messages it reports word for word, from the start to the end of the call, or of its go or defer statement,
// where staticcheck's report puts it, with one related entry at the call that returned the directory. The original
// walks every source function of a package in one run, but keeps nothing from one function to the next: it judges
// each call to os.RemoveAll by the one value its argument flattens to. So the port judges one function at a time and
// finds what the original finds. The registry runs the port in the original's place, under its name, and the original
// stays only as the oracle: the parity tests beside this file run both over the same packages, related entries
// included, and they differ nowhere. The original reports through staticcheck's report with its Related option; the
// port sets the one related entry itself until the shared report kit (#wtvf02h) carries it.
//
// The related entry names, as an example, the directory the call returns on the machine the check runs on, which the
// original reads from the process's environment while it runs; so does the port, which is the one thing it reads
// beyond its function and rule.Context. Where the directory cannot be found, both leave the example out. The one place
// the port could differ is a call with no syntax, where staticcheck's report panics and the port reports nothing;
// staticcheck builds every call it judges here from its expression or statement, so no such call reaches either.
package removed_user_directory

import (
	"fmt"
	"os"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	FileLocal:                  true,
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA9007",
	"SA9007",
	"Flags a call to os.RemoveAll that deletes the temporary directory or the user's cache, config or home "+
		"directory itself: staticcheck's SA9007, ported onto the engine's own "+
		"intermediate representation.",
	&Listener,
)

// userDirectory is one call that returns a directory the check guards, by what its messages read.
type userDirectory struct {
	// returning is the call that returns the directory, its result, or for a call of two results the first of them,
	// where the related entry points.
	returning intermediate_representation.Instruction
	// callName is the full name of the function called, os.TempDir or one of os.UserCacheDir, os.UserConfigDir and
	// os.UserHomeDir.
	callName string
	// kind is the directory's kind as the messages name it: temporary, cache, config or home.
	kind string
	// example is the directory the call returns on this machine, or empty where it cannot be found.
	example string
}

// Check judges one function: each call to os.RemoveAll, made, started as a goroutine or deferred, whose argument is,
// on every path, the result of one call to a function that returns a directory the check guards.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			call, isCall := instruction.(intermediate_representation.CallInstruction)
			if !isCall || !intermediate_representation.IsCallTo(call.Common(), "os.RemoveAll") {
				continue
			}
			directory, isGuarded := guardedDirectory(call.Common().Arguments[0])
			if !isGuarded {
				continue
			}
			report(context, call, directory)
		}
	}
}

// guardedDirectory is the directory argument holds, and whether it is the result of a call that returns a directory the
// check guards. The argument is flattened through its phis first, so a directory merged with itself is still found, and
// one merged with any other value, a subdirectory joined onto it on some path, is not: on such a path the removal may
// be right, and the original leaves it alone rather than report a path that may never run.
func guardedDirectory(argument intermediate_representation.Value) (userDirectory, bool) {
	switch value := intermediate_representation.Flatten(argument).(type) {
	case *intermediate_representation.Call:
		if intermediate_representation.CallName(&value.Call) != "os.TempDir" {
			return userDirectory{}, false
		}
		return userDirectory{returning: value, callName: "os.TempDir", kind: "temporary", example: os.TempDir()}, true
	case *intermediate_representation.Extract:
		// The directory is the first of the two results; the second is the error.
		if value.Index != 0 {
			return userDirectory{}, false
		}
		returning, isCall := value.Tuple.(*intermediate_representation.Call)
		if !isCall {
			return userDirectory{}, false
		}
		callName := intermediate_representation.CallName(&returning.Call)
		directory := userDirectory{returning: value, callName: callName}
		switch callName {
		case "os.UserCacheDir":
			directory.kind = "cache"
			// Ignored: an error leaves the example empty, which the message then leaves out, as the original's does.
			directory.example, _ = os.UserCacheDir()
		case "os.UserConfigDir":
			directory.kind = "config"
			// Ignored: an error leaves the example empty, which the message then leaves out, as the original's does.
			directory.example, _ = os.UserConfigDir()
		case "os.UserHomeDir":
			directory.kind = "home"
			// Ignored: an error leaves the example empty, which the message then leaves out, as the original's does.
			directory.example, _ = os.UserHomeDir()
		default:
			return userDirectory{}, false
		}
		return directory, true
	}
	// Flatten is nil for an argument merged from more than one value, and any other value is no such call.
	return userDirectory{}, false
}

// report reports call, which removes directory, with directory's returning call as its related entry, as
// staticcheck's report with its Related option does.
func report(context *rule.Context, call intermediate_representation.CallInstruction, directory userDirectory) {
	source := call.Source()
	if source == nil {
		// A call with no syntax has no place to report; staticcheck's report panics on one.
		return
	}
	diagnostic := analysis.Diagnostic{
		Pos: source.Pos(),
		End: source.End(),
		Message: fmt.Sprintf("this call to os.RemoveAll deletes the user's entire %s directory, not a subdirectory "+
			"therein", directory.kind),
	}
	if returningSource := directory.returning.Source(); returningSource != nil {
		// Staticcheck's Related option leaves out an entry with no syntax and reports the finding without it.
		relatedMessage := fmt.Sprintf("this call to %s returns the user's %s directory", directory.callName,
			directory.kind)
		if directory.example != "" {
			relatedMessage = fmt.Sprintf("%s, for example %s", relatedMessage, directory.example)
		}
		diagnostic.Related = []analysis.RelatedInformation{{
			Pos:     returningSource.Pos(),
			End:     returningSource.End(),
			Message: relatedMessage,
		}}
	}
	context.Report(diagnostic)
}
