// Command extract_oxc_fixtures reads oxc's inline rule corpora and reports what they contain.
//
// This is about to become the source of truth for 119 unported rules, so what it says about itself
// matters more than what it extracts. Two properties are load-bearing and both are about honesty
// rather than coverage.
//
// # The count does not come from the input list
//
// The number a fixture must assert is the diagnostic count, and it is not the number of inputs.
// Measured across four rules on one day:
//
//	no-obj-calls               40 fail inputs   42 diagnostics
//	no-global-assign            8 fail inputs    9 diagnostics
//	no-irregular-whitespace    58 fail inputs   83 diagnostics
//	no-regex-spaces            26 fail inputs   26 diagnostics
//
// One input can report twice, and oxc emits per character where ESLint emits per run. **An importer
// assuming one finding per input is wrong on three of those four.** This is written here rather than
// only in the code because the loop that reconciles two files against each other looks redundant
// until you know that, and the obvious simplification is silently wrong.
//
// # A case in the blocks and absent from the snapshot is a finding, not a row to skip
//
// Not every case reaches a snapshot. `no-irregular-whitespace`'s second Tester block calls `.test()`
// rather than `.test_and_snapshot()`, so its two cases produce no snapshot entry at all, and both
// are regression cases somebody added deliberately. An extractor keyed on the snapshot drops them
// and reports a clean run.
//
// So this tool reports what it read rather than what it intended to read: blocks found, cases
// extracted, snapshot entries reconciled, and every case it could not account for with the reason.
// A silent zero here would be the most expensive one available, because it would look exactly like a
// rule validated against upstream.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// corpus is what one Tester block declares.
type corpus struct {
	// blockIndex distinguishes the second Tester block from the first, which is the case that hides
	// a config surface and, in one measured rule, two regression cases outside the snapshot.
	blockIndex int
	// snapshotted records whether this block calls test_and_snapshot rather than test. A block that
	// does not snapshot contributes cases whose diagnostic count nothing states.
	snapshotted bool
	pass        []fixtureCase
	fail        []fixtureCase
}

// fixtureCase is one corpus entry: the source text, and the options it was run under.
type fixtureCase struct {
	source string
	// options is the second tuple element verbatim, or empty for None. Dropping it converts an
	// options case into a defaults case, which passes while asserting nothing about the option.
	// Not rare: 135 of use-isnan's cases carry one, and 50 of no-irregular-whitespace's.
	options string
}

func main() {
	rulesDirectory := flag.String("rules", "", "oxc eslint rules directory")
	snapshotDirectory := flag.String("snapshots", "", "oxc snapshots directory")
	rule := flag.String("rule", "", "rule file stem, e.g. use_isnan")
	snapshotPrefix := flag.String("prefix", "", "snapshot filename prefix; defaults to the rules directory's name")
	flag.Parse()

	if *rulesDirectory == "" || *snapshotDirectory == "" || *rule == "" {
		fmt.Fprintln(os.Stderr, "usage: extract_oxc_fixtures -rules <dir> -snapshots <dir> -rule <stem>")
		os.Exit(2)
	}

	rulePath := filepath.Join(*rulesDirectory, *rule+".rs")
	source, err := os.ReadFile(rulePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read %s: %v\n", rulePath, err)
		os.Exit(1)
	}

	blocks := readTesterBlocks(string(source))
	if len(blocks) == 0 {
		// A rule file with no Tester block is a real state, and reporting it as an empty corpus
		// would read identically to a successful extraction of nothing.
		fmt.Printf("%s: NO TESTER BLOCK FOUND — this is a finding, not an empty corpus\n", *rule)
		os.Exit(1)
	}

	// The snapshot prefix is the plugin directory's name rather than a constant. It was hardcoded to
	// `eslint_` while the tool was validated against core rules, where that is correct, and it
	// reported every snapshot absent the first time it ran against nextjs, react and typescript.
	// The zero was in the right column and meant nothing, which is this tool's own failure mode
	// arriving in its own code.
	plugin := *snapshotPrefix
	if plugin == "" {
		plugin = filepath.Base(*rulesDirectory)
	}
	snapshotPath := filepath.Join(*snapshotDirectory, plugin+"_"+*rule+".snap")
	diagnostics, snapshotExists := countSnapshotDiagnostics(snapshotPath)

	fmt.Printf("rule            %s\n", *rule)
	fmt.Printf("tester blocks   %d\n", len(blocks))

	totalPass, totalFail, unsnapshotted := 0, 0, 0
	for _, block := range blocks {
		totalPass += len(block.pass)
		totalFail += len(block.fail)
		marker := "test_and_snapshot"
		if !block.snapshotted {
			marker = "test — NOT snapshotted"
			unsnapshotted += len(block.fail)
		}
		fmt.Printf("  block %d       %d pass, %d fail  (%s)\n",
			block.blockIndex, len(block.pass), len(block.fail), marker)
	}

	fmt.Printf("cases extracted %d pass, %d fail\n", totalPass, totalFail)

	if totalPass == 0 && totalFail == 0 {
		// A rule with a Tester block and no extractable cases is not an empty corpus, and saying so
		// would be the silent zero this tool exists to prevent. Two of 184 eslint rules build their
		// vectors programmatically — `let mut pass = Vec::<TestCase>::new()` followed by a loop that
		// pushes generated sources and an `.extend([...])` of written ones. Some of their cases do
		// not exist as source text at all, so no parser can recover them and a hand copy is the only
		// honest route.
		fmt.Printf("                NOT EXTRACTABLE: a Tester block exists and no case could be read " +
			"from it. Expect a programmatically built vector (Vec::new plus push/extend); such a " +
			"corpus must be copied by hand, and part of it may be generated rather than written\n")
	}

	if !snapshotExists {
		fmt.Printf("snapshot        ABSENT at %s — diagnostic counts unstated\n", snapshotPath)
	} else {
		fmt.Printf("snapshot        %d diagnostics from %d fail inputs\n", diagnostics, totalFail)
		if diagnostics != totalFail-unsnapshotted {
			// Reported rather than reconciled away. This is the number a fixture must assert, and
			// the gap is the finding: an input reporting twice, or per-character emission.
			fmt.Printf("                DISCREPANCY: %d diagnostics against %d snapshotted inputs; "+
				"a fixture asserting one finding per input would be wrong\n",
				diagnostics, totalFail-unsnapshotted)
		}
	}
	if unsnapshotted > 0 {
		fmt.Printf("dropped         %d fail cases in a non-snapshotting block; their diagnostic "+
			"count is stated nowhere and they are usually regressions\n", unsnapshotted)
	}

	withOptions := 0
	for _, block := range blocks {
		for _, entry := range append(append([]fixtureCase{}, block.pass...), block.fail...) {
			if entry.options != "" {
				withOptions++
			}
		}
	}
	fmt.Printf("with options    %d cases carry a second tuple element\n", withOptions)
}

// testerBlockPattern finds each Tester::new invocation and how it is run.
var testerBlockPattern = regexp.MustCompile(`Tester::new\([^)]*\)`)

// readTesterBlocks splits a rule file's test function into its Tester blocks.
//
// Counting the blocks before the cases is the amendment a wave paid for: 36 of 946 oxc rule files
// carry two or more, and a single-corpus assumption drops one silently.
func readTesterBlocks(source string) []corpus {
	locations := testerBlockPattern.FindAllStringIndex(source, -1)
	if len(locations) == 0 {
		return nil
	}

	blocks := make([]corpus, 0, len(locations))
	for index, location := range locations {
		end := len(source)
		if index+1 < len(locations) {
			end = locations[index+1][0]
		}
		// A block's own text runs to the next Tester or the end of the file. The pass and fail
		// vectors precede the Tester call in this file's style, so the segment searched for them
		// starts at the previous block's end rather than at this call.
		start := 0
		if index > 0 {
			start = locations[index-1][1]
		}
		segment := source[start:location[1]]
		trailing := source[location[1]:end]

		blocks = append(blocks, corpus{
			blockIndex:  index + 1,
			snapshotted: strings.Contains(trailing, "test_and_snapshot"),
			pass:        readVector(segment, "let pass"),
			fail:        readVector(segment, "let fail"),
		})
	}
	return blocks
}

// readVector pulls the tuple entries out of one `let pass = vec![...]` or `let fail = vec![...]`.
//
// Rust raw strings (`r#"..."#`) exist precisely to carry embedded quotes, so a scan for the next
// quote character closes a string the parser considers open. Both forms are handled here rather
// than approximated, because the cases carrying embedded quotes are the interesting ones.
func readVector(segment string, marker string) []fixtureCase {
	start := strings.Index(segment, marker)
	if start < 0 {
		return nil
	}
	open := strings.Index(segment[start:], "vec![")
	if open < 0 {
		return nil
	}
	body, ok := balancedSlice(segment[start+open+len("vec!"):], '[', ']')
	if !ok {
		return nil
	}
	return readTuples(body)
}

// balancedSlice returns the text between the first opening delimiter and its match.
//
// String-aware, because a bracket inside a fixture's source text is not a delimiter. That is not
// hypothetical: no-regex-spaces carries `"var foo = new RegExp(' [   ');"` as a pass case, and a
// naive bracket walk on that file reports the wrong end.
func balancedSlice(text string, open rune, close rune) (string, bool) {
	depth := 0
	inString := false
	inRaw := false
	var stringDelimiter rune

	// The opening delimiter's position is recorded in rune space as the walk passes it. It was
	// originally recovered afterwards with strings.IndexRune, which returns a BYTE offset and was
	// being used to slice a []rune. Every multi-byte character before the bracket shifted the
	// result, and these corpora are full of them: no-irregular-whitespace's cases are literal
	// Unicode space characters. The counts it produced were wrong in a way that looked plausible.
	openIndex := -1

	runes := []rune(text)
	for index := 0; index < len(runes); index++ {
		current := runes[index]

		if inRaw {
			if current == '"' && index+1 < len(runes) && runes[index+1] == '#' {
				inRaw = false
				index++
			}
			continue
		}
		if inString {
			if current == '\\' {
				index++
				continue
			}
			if current == stringDelimiter {
				inString = false
			}
			continue
		}

		// A commented-out case is not a case. oxc parks disabled fixtures inline, and they hold
		// string literals that read as live entries to a walker that only tracks quotes: 21 of
		// no-obj-calls' are capability gaps sitting directly beneath the live ones. Counting them
		// inflated that rule's fail vector from 40 to 96 while looking entirely plausible.
		if current == '/' && index+1 < len(runes) && runes[index+1] == '/' {
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
			continue
		}

		switch {
		case current == 'r' && index+2 < len(runes) && runes[index+1] == '#' && runes[index+2] == '"':
			inRaw = true
			index += 2
		case current == '"' || current == '\'':
			// A lifetime tick is not a string opener, but no fixture vector contains one, and
			// treating it as a string would unbalance the walk rather than fail quietly.
			inString = true
			stringDelimiter = current
		case current == open:
			depth++
			if depth == 1 {
				openIndex = index
				continue
			}
		case current == close:
			depth--
			if depth == 0 && openIndex >= 0 {
				return string(runes[openIndex+1 : index]), true
			}
		}
	}
	return "", false
}

// readTuples splits a vector body into its top-level entries.
//
// **A vector holds two shapes and assuming one is wrong.** Some rules write every case as a
// `(source, options)` tuple; others write bare string literals when no case takes options; and a
// rule can mix them. `no-obj-calls` writes bare strings throughout, and a reader counting only
// parenthesised entries finds zero pass cases there while still reporting a number.
//
// That is how this function was first written, and it produced 0 pass / 52 fail against a hand
// count of 35 / 40. It is the same defect this tool exists to catch, one level up: a probe that ran
// perfectly against a shape the corpus does not use.
func readTuples(body string) []fixtureCase {
	var cases []fixtureCase
	runes := []rune(body)

	for index := 0; index < len(runes); index++ {
		current := runes[index]

		// Same reason as in balancedSlice: a disabled case is not a case.
		if current == '/' && index+1 < len(runes) && runes[index+1] == '/' {
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
			continue
		}

		switch {
		case current == '(':
			entry, ok := balancedSlice(string(runes[index:]), '(', ')')
			if !ok {
				continue
			}
			source, options := splitTuple(entry)
			cases = append(cases, fixtureCase{source: source, options: options})
			index += len([]rune(entry)) + 1

		case current == 'r' && index+2 < len(runes) && runes[index+1] == '#' && runes[index+2] == '"',
			current == '"':
			// A bare string literal is a case with no options. Consumed whole so that a bracket or
			// comma inside the fixture's own source text is not read as structure.
			entry, ok := readStringLiteral(runes[index:])
			if !ok {
				continue
			}
			cases = append(cases, fixtureCase{source: entry})
			index += len([]rune(entry)) - 1
		}
	}
	return cases
}

// readStringLiteral consumes one Rust string literal, plain or raw, and returns its full text.
func readStringLiteral(runes []rune) (string, bool) {
	if len(runes) == 0 {
		return "", false
	}

	if runes[0] == 'r' {
		for index := 3; index < len(runes); index++ {
			if runes[index] == '"' && index+1 < len(runes) && runes[index+1] == '#' {
				return string(runes[:index+2]), true
			}
		}
		return "", false
	}

	for index := 1; index < len(runes); index++ {
		if runes[index] == '\\' {
			index++
			continue
		}
		if runes[index] == '"' {
			return string(runes[:index+1]), true
		}
	}
	return "", false
}

// splitTuple separates a case's source text from its options element.
//
// The options half is preserved verbatim rather than parsed. Dropping it is the failure worth
// guarding: an options case silently becomes a defaults case, passes, and asserts nothing about the
// option it exists to cover.
func splitTuple(entry string) (source string, options string) {
	depth := 0
	inString := false
	inRaw := false
	var stringDelimiter rune

	runes := []rune(entry)
	for index := 0; index < len(runes); index++ {
		current := runes[index]

		if inRaw {
			if current == '"' && index+1 < len(runes) && runes[index+1] == '#' {
				inRaw = false
				index++
			}
			continue
		}
		if inString {
			if current == '\\' {
				index++
				continue
			}
			if current == stringDelimiter {
				inString = false
			}
			continue
		}

		switch {
		case current == 'r' && index+2 < len(runes) && runes[index+1] == '#' && runes[index+2] == '"':
			inRaw = true
			index += 2
		case current == '"' || current == '\'':
			inString = true
			stringDelimiter = current
		case current == '(' || current == '[' || current == '{':
			depth++
		case current == ')' || current == ']' || current == '}':
			depth--
		case current == ',' && depth == 0:
			rest := strings.TrimSpace(string(runes[index+1:]))
			if rest == "None" || rest == "None," {
				return strings.TrimSpace(string(runes[:index])), ""
			}
			return strings.TrimSpace(string(runes[:index])), rest
		}
	}
	return strings.TrimSpace(entry), ""
}

// countSnapshotDiagnostics counts the diagnostics oxc recorded for a rule.
//
// The marker is a warning sign rather than a multiplication sign, and 844 of 867 snapshots use it.
// A probe counting the wrong character returns a silent zero on the large majority of rules, and the
// zero reads as a clean corpus. That cost another node most of a pass before it was found.
func countSnapshotDiagnostics(path string) (int, bool) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	return strings.Count(string(contents), "⚠"), true
}
