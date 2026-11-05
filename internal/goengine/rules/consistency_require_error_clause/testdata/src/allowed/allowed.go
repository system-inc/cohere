package allowed

import (
	"errors"
	"fmt"
)

var cause = errors.New("permission denied")

// A lowercase clause, with or without a wrapped cause, is the house shape.
func clauses() []error {
	return []error{
		errors.New("opening config"),
		fmt.Errorf("reading %s: %w", "path", cause),
		errors.New(`raw text in a lowercase clause`),
		errors.New("opening config: permission denied"),
		errors.New("reading v1.2 of the schema"),
		errors.New("closing the stream (twice)"),
		errors.New("is this the right file?"),
		errors.New("the file is locked"),
		errors.New("couldn't insert the row"),
	}
}

// A capitalized word outside the vocabulary may be a proper name, a language, a tool, a type here or in another
// package, and its shape cannot tell, so it is never flagged, whatever follows it.
func properNamesByAnyShape() []error {
	return []error{
		errors.New("Swift is not available on this machine"),
		errors.New("Prettier config remains unresolved"),
		errors.New("Node.Text on a nil node"),
		errors.New("Analyzer has no rules"),
		errors.New("JSON is malformed"),
		errors.New("TypeScript build broke"),
		errors.New("Go toolchain not found"),
		errors.New("Opening config"),
		errors.New("I/O stalled"),
		errors.New("It's locked"),
	}
}

// A vocabulary word with a capital after its first letter is an initialism or a code, not a sentence's capital.
func initialismsSpellingAWord() []error {
	return []error{
		errors.New("IT department mailbox is full"),
		errors.New("NO_PROXY is set twice"),
		errors.New("ON clause has no columns"),
		errors.New("IF statement in the template is unclosed"),
	}
}

// A vocabulary word that only opens a longer identifier is read whole, as the identifier, which is no word.
func identifiersOpeningOnAWord() []error {
	return []error{
		errors.New("No_proxy is set twice"),
		errors.New("Not2 is reserved"),
		errors.New("Thereby is no word here"),
		errors.New("Ann is unknown to the roster"),
	}
}

// A text that opens on no letter has no capital to judge, and an empty text has nothing to judge at all.
func noOpeningWord() []error {
	return []error{
		fmt.Errorf("%s: not found", "path"),
		fmt.Errorf("%w", cause),
		errors.New("404 from the server"),
		errors.New(""),
		errors.New("   "),
	}
}

// A clause that only resembles a failure prefix opens on a different word.
func nearPrefixes() []error {
	return []error{
		errors.New("failures exceeded the limit"),
		errors.New("failed tokenizing the input"),
		errors.New("error reading the cache"),
		errors.New("errors piled up in the queue"),
		errors.New("could nothing be done"),
		errors.New("unable tomorrow"),
		errors.New("reading config: failed to open"),
	}
}
