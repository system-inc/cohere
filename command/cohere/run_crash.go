package main

import "fmt"

// runCrash is a rule that could not finish a file, or a file nothing could check, because cohere crashed.
//
// A crash is cohere's bug, never the user's, and a verdict it leaves missing fails the run (#v1ah2qq).
// It used to be named only under --verbose while the run ended green: TanStack/query's six react-hooks
// crashes printed a footer marker and nothing else, so a missing verdict read like a clean one. The README
// already promised that a run which could not check a file never exits 0.
type runCrash struct {
	Path string `json:"path"`
	// Rule is the rule that crashed, empty when the whole file was lost.
	Rule  string `json:"rule,omitempty"`
	Cause string `json:"cause"`
	// Engine names the contract engine that could not finish the file, as a reader says it ("Swift", "Go"),
	// and is empty for a TypeScript crash. Such a crash's Cause is the engine's own sentence, and it is never a
	// panic of cohere's: the Swift engine reports a file its typed rules had no index for, the Go engine a rule
	// that failed on a package, and each says which rule it cost while every other rule ran. Wrapping that in
	// cohere's crash words would say nothing was checked and call it a bug to report, so message says the
	// engine's sentence instead. Kept out of `--json`, where the cause already is that sentence.
	Engine string `json:"-"`
}

// message says what crashed, that it is cohere's bug, what is missing because of it, and where to report it.
// For a file a contract engine could not finish, it says the engine's sentence, which already names what is
// missing.
func (crash runCrash) message() string {
	if crash.Engine != "" {
		return "the " + crash.Engine + " engine could not finish this file: " + crash.Cause
	}
	what := fmt.Sprintf("cohere's rule %s crashed on this file (%s). This is a bug in cohere, not in your code: "+
		"the rule's verdict on this file is missing, and the file's other rules ran.", crash.Rule, crash.Cause)
	if crash.Rule == "" {
		what = fmt.Sprintf("cohere crashed on this file (%s). This is a bug in cohere, not in your code: "+
			"nothing in this file was checked.", crash.Cause)
	}
	return what + fmt.Sprintf(" Please report it at %s with the output of `cohere --version`, the rule, "+
		"the code that crashed it, and this message.", reportBugsAt)
}
