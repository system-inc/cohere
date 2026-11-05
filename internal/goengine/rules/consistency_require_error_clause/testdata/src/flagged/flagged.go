package flagged

import (
	"errors"
	stderrors "errors"
	"fmt"
)

var cause = errors.New("permission denied")

// A capital on a plain word lands mid-line once the error is wrapped, through either constructor.
func capitalized() []error {
	return []error{
		errors.New("Opening config"),                // want `opens with .Opening., and an error's text`
		fmt.Errorf("Reading %s: %w", "path", cause), // want `opens with .Reading., and an error's text`
		errors.New(`Raw text opens on a capital`),   // want `opens with .Raw., and an error's text`
	}
}

// A single capital letter is no initialism, and an exported word that names nothing in scope is no proper name.
func notProperNames() []error {
	return []error{
		errors.New("I/O stalled"),          // want `opens with .I., and an error's text`
		errors.New("Parser stopped early"), // want `opens with .Parser., and an error's text`
		errors.New("Go_away is no name"),   // want `opens with .Go_away., and an error's text`
	}
}

// A local declared after the call is not in scope where the error is made, so it excuses nothing.
func declaredAfter() error {
	made := errors.New("Later broke") // want `opens with .Later., and an error's text`
	Later := made
	return Later
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
		errors.New("Opening config."), // want `opens with .Opening., and an error's text` `ends with .\.., and`
		errors.New("could not open."), // want `opens with .could not., which says only` `ends with .\.., and`
	}
}

// Literals joined across lines are still the text written where the error is made, and a constructor imported under
// another name or called through parentheses is still the standard library's.
func shapes() []error {
	return []error{
		errors.New("Opening the config " + // want `opens with .Opening., and an error's text`
			"file"),
		errors.New(("Wrapped in parentheses")), // want `opens with .Wrapped., and an error's text`
		stderrors.New("Renamed import"),        // want `opens with .Renamed., and an error's text`
		(fmt.Errorf)("Parenthesized callee"),   // want `opens with .Parenthesized., and an error's text`
	}
}
