package flagged

import (
	"errors"
	stderrors "errors"
	"fmt"
)

var cause = errors.New("permission denied")

// An ordinary word with a sentence's capital lands mid-line once the error is wrapped, through either constructor.
func capitalized() []error {
	return []error{
		errors.New("Cannot read the config"),             // want `opens with .Cannot., capitalized as if`
		fmt.Errorf("Couldn't insert %s: %w", "x", cause), // want `opens with .Couldn't., capitalized as if`
		errors.New("The file is locked"),                 // want `opens with .The., capitalized as if`
		errors.New(`Invalid header in the raw text`),     // want `opens with .Invalid., capitalized as if`
	}
}

// Each group of the vocabulary: a determiner, a single-letter article, a modal, a negation, an error opener, a
// preposition, and a word alone with nothing after it.
func groups() []error {
	return []error{
		errors.New("No such file"),              // want `opens with .No., capitalized as if`
		errors.New("A lock is held"),            // want `opens with .A., capitalized as if`
		errors.New("Must hold the lock"),        // want `opens with .Must., capitalized as if`
		errors.New("Not a directory"),           // want `opens with .Not., capitalized as if`
		errors.New("Unexpected end of input"),   // want `opens with .Unexpected., capitalized as if`
		errors.New("Failed reading the cache"),  // want `opens with .Failed., capitalized as if`
		errors.New("Error reading the cache"),   // want `opens with .Error., capitalized as if`
		fmt.Errorf("While reading %s", "path"),  // want `opens with .While., capitalized as if`
		errors.New("Missing"),                   // want `opens with .Missing., capitalized as if`
		errors.New("Empty, so nothing to read"), // want `opens with .Empty., capitalized as if`
	}
}

// Each mark that ends the text is reported, trailing space and a newline after it included.
func trailing() []error {
	return []error{
		errors.New("opening config."),             // want `ends with .\.., and an error's text`
		errors.New("opening config!"),             // want `ends with .!., and an error's text`
		fmt.Errorf("opening config:"),             // want `ends with .:., and an error's text`
		fmt.Errorf("reading %s.\n", "path"),       // want `ends with .\.., and an error's text`
		errors.New("waiting for the lock... "),    // want `ends with .\.., and an error's text`
		fmt.Errorf("reading %q: %w.", "x", cause), // want `ends with .\.., and an error's text`
	}
}

// Every failure prefix, in any case.
func failurePrefixes() []error {
	return []error{
		errors.New("failed to open config"),      // want `opens with .failed to., which says only`
		errors.New("failure reading the cache"),  // want `opens with .failure., which says only`
		errors.New("error: no such file"),        // want `opens with .error:., which says only`
		errors.New("err:no such file"),           // want `opens with .err:., which says only`
		fmt.Errorf("unable to dial %s", "host"),  // want `opens with .unable to., which says only`
		fmt.Errorf("could not parse: %w", cause), // want `opens with .could not., which says only`
		errors.New("ERROR: shouted"),             // want `opens with .ERROR:., which says only`
		errors.New("failure"),                    // want `opens with .failure., which says only`
	}
}

// A capitalized failure prefix is one problem, since dropping the prefix drops the capital with it.
func capitalizedPrefix() error {
	return errors.New("Failed to open config") // want `opens with .Failed to., which says only`
}

// Two problems in one text are two findings, each with its own id.
func twoProblems() []error {
	return []error{
		errors.New("The config is missing."), // want `opens with .The., capitalized as if` `ends with .\.., and`
		errors.New("could not open."),        // want `opens with .could not., which says only` `ends with .\.., and`
	}
}

// Literals joined across lines are still the text written where the error is made, and a constructor imported under
// another name or called through parentheses is still the standard library's.
func shapes() []error {
	return []error{
		errors.New("The config file " + // want `opens with .The., capitalized as if`
			"is locked"),
		errors.New(("Invalid in parentheses")),   // want `opens with .Invalid., capitalized as if`
		stderrors.New("Unknown renamed import"),  // want `opens with .Unknown., capitalized as if`
		(fmt.Errorf)("Bad parenthesized callee"), // want `opens with .Bad., capitalized as if`
	}
}
