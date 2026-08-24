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
	"strconv"
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
	// Two clones wrote throwaway dumpers before this existed, because the counts above tell a porter
	// how many cases there are and step 5 of the brief requires the cases themselves, verbatim. The
	// parser that produced the counts already holds them, so printing them costs one flag and
	// removes the hand transcription that a third dumper would have reintroduced.
	dump := flag.Bool("dump", false, "print every extracted case verbatim, one per line, Go-quoted")
	flag.Parse()

	if *rulesDirectory == "" || *snapshotDirectory == "" || *rule == "" {
		fmt.Fprintln(os.Stderr, "usage: extract_oxc_fixtures -rules <dir> -snapshots <dir> -rule <stem>")
		os.Exit(2)
	}

	// A rule can be a directory rather than a file, and the largest ones are: no-unused-vars is
	// 10,064 lines across an implementation, an options module, a fixer subtree and a tests
	// directory. Reading only `<rule>.rs` found nothing there and printed nothing, which is a silent
	// zero inside the tool built to refuse silent zeros. Found when the option-key check reported
	// clean on a corpus known to carry a misconfigured case.
	rulePath := filepath.Join(*rulesDirectory, *rule+".rs")
	source, err := os.ReadFile(rulePath)
	if err != nil {
		combined, readErr := readRuleDirectory(filepath.Join(*rulesDirectory, *rule))
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "cannot read %s as a file or a directory: %v\n", rulePath, err)
			os.Exit(1)
		}
		source = []byte(combined)
		fmt.Printf("layout          directory rather than a single file\n")
	}

	fixCases := countFixVector(string(source))
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
	var optionKeys []string
	for _, block := range blocks {
		for _, entry := range append(append([]fixtureCase{}, block.pass...), block.fail...) {
			if entry.options == "" {
				continue
			}
			withOptions++
			optionKeys = append(optionKeys, keysIn(entry.options)...)
		}
	}
	fmt.Printf("with options    %d cases carry a second tuple element\n", withOptions)

	if fixCases > 0 {
		// The fix vector is the highest-value artifact in a fixable rule's file and this tool did not
		// read it for a long time. A clone porting a rule with 382 input/output pairs found them
		// invisible here and had to pull them out by hand, which is the same waste the case dumper
		// exists to prevent. Counting them at least tells a porter the vector is there.
		fmt.Printf("fix vector      %d before/after pairs, which assert the repair rather than the "+
			"finding; extract them separately and assert with ExpectFixedSource\n", fixCases)
	}

	if *dump {
		// Go-quoted rather than raw, so a case carrying a newline, a tab or a quote survives being
		// read off a terminal and pasted into a fixture table. The block and options columns come
		// along because a case's block decides whether the snapshot counts it and a case's options
		// decide whether it tests what it looks like it tests.
		for _, block := range blocks {
			for _, entry := range block.pass {
				fmt.Printf("PASS block=%d options=%s %s\n", block.blockIndex, quoteOrNone(entry.options), strconv.Quote(rustLiteralText(entry.source)))
			}
			for _, entry := range block.fail {
				fmt.Printf("FAIL block=%d options=%s %s\n", block.blockIndex, quoteOrNone(entry.options), strconv.Quote(rustLiteralText(entry.source)))
			}
		}
	}

	for _, key := range unrecognizedKeys(optionKeys, string(source), rulesDirectoryFor(rulePath)) {
		// A case configuring a key the rule never reads runs on defaults while wearing the shape of
		// a case that proves the option works, and it passes. oxc's own no-unused-vars corpus has
		// one: it writes `reportUnusedIgnorePattern` where the option is `reportUsedIgnorePattern`,
		// and the rule's `deny_unknown_fields` is decorative, so nothing rejects it.
		//
		// Importing faithfully imports the hole. Reported rather than dropped, same as a case absent
		// from the snapshot.
		fmt.Printf("                UNRECOGNIZED OPTION KEY %q: it appears in a case and nowhere in "+
			"the rule's own source, so that case runs on defaults while looking like it tests the "+
			"option\n", key)
	}
}

// rustLiteralText strips a Rust string literal's delimiters, leaving the source the case actually
// runs.
//
// A case is stored with its delimiters because everything before -dump only counted cases, so the
// wrapper never mattered. It matters here: a raw string comes back as `r#"..."#` and a porter
// pasting that into a Go fixture gets the wrapper too. The raw form is left otherwise untouched,
// since a raw string is exactly the bytes between its delimiters, and a plain literal is unquoted
// so that its escapes become the characters they denote.
func rustLiteralText(literal string) string {
	if strings.HasPrefix(literal, "r") {
		// `r"..."` carries zero hashes and `r#"..."#` carries one, so the count is read rather
		// than assumed: oxc uses both forms, and a fixed guess of one leaves the quotes on the
		// zero-hash cases, which is what the first version of this did.
		hashes := 0
		for 1+hashes < len(literal) && literal[1+hashes] == '#' {
			hashes++
		}
		opening := 1 + hashes + 1
		closing := len(literal) - 1 - hashes
		if opening <= closing {
			return literal[opening:closing]
		}
		return literal
	}
	// Go and Rust agree on the escapes these fixtures use, so Go's unquoter is the right reader;
	// anything it rejects is returned as written rather than silently mangled.
	if unquoted, err := strconv.Unquote(literal); err == nil {
		return unquoted
	}

	// Rust's zero-hash raw form `r"..."` reaches here with its `r` already dropped by the scanner's
	// dispatch, which tests for `r#"` specifically. Its body is raw, so a backslash inside it is a
	// backslash and Go's unquoter rejects the whole literal rather than mangling it. Two of
	// no-obj-calls's clean cases are that form and both carry real newlines. Widening the dispatch
	// to accept a bare `r` was tried and is wrong: it matches the identifier `r` in
	// `let area = r => 2 * Math.PI * r * r`, which swallowed a whole vector and took the pass count
	// from 35 to 0. Trimming the delimiters here is the narrow fix, and it leaves the counts alone.
	if len(literal) >= 2 && literal[0] == '"' && literal[len(literal)-1] == '"' {
		// The body still carries Rust's escapes, and this path is reached precisely because Go's
		// unquoter refused the whole literal. Returning it as written meant `-dump` re-escaped an
		// already-escaped body: a source holding `ref=\"hello\"` printed as `ref=\\"hello\\"`, so a
		// porter decoding it got a literal backslash and a fixture testing a different attribute
		// value while sitting green. Found by a porter comparing bytes rather than reading.
		//
		// Unescaping the two sequences that actually appear is the narrow fix. Anything else is
		// left alone, because guessing at an escape this reader does not understand is how the
		// mangling started.
		body := literal[1 : len(literal)-1]
		body = strings.ReplaceAll(body, `\"`, `"`)
		body = strings.ReplaceAll(body, `\\`, `\`)
		return body
	}
	return literal
}

// quoteOrNone renders a case's options column for -dump, distinguishing a case with no second
// tuple element from one whose options happen to be empty text.
func quoteOrNone(options string) string {
	if options == "" {
		return "none"
	}
	return strconv.Quote(options)
}

// optionKeyPattern finds the quoted keys of a serde_json object literal.
var optionKeyPattern = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*)"\s*:`)

// keysIn returns the option keys a case names, and only those.
//
// A case can carry three elements, not two: source, rule options, and lint configuration. The third
// holds `env`, `globals` and the global names inside them, which are settings for the harness rather
// than options the rule reads. Scanning the whole tail reported twenty of those as unrecognized
// across the eslint directory, every one a false positive, and a check that fires on correct rules
// teaches a reader to skip it.
//
// So only the first element is read, and only when it is a serde_json literal. Anything past its
// closing paren belongs to the harness.
func keysIn(options string) []string {
	// Both macro spellings appear in the corpus. The tests directory of a multi-file rule imports
	// the macro and writes `json!`, while single-file rules write it qualified. Matching only the
	// qualified form excluded the one case this check exists to catch.
	trimmed := strings.TrimSpace(options)
	if !strings.HasPrefix(trimmed, "Some(serde_json::json!") && !strings.HasPrefix(trimmed, "Some(json!") {
		return nil
	}
	body, ok := balancedSlice(trimmed, '(', ')')
	if !ok {
		return nil
	}

	var keys []string
	for _, match := range optionKeyPattern.FindAllStringSubmatch(body, -1) {
		keys = append(keys, match[1])
	}
	return keys
}

// rulesDirectoryFor returns the directory holding a rule, so a multi-file rule's siblings are read.
//
// A rule can be a directory rather than a file, and no-unused-vars is: its options live in
// options.rs beside the rule. Reading only the rule file would find no real key there and report
// every one of them, which is the false-positive direction of this check.
func rulesDirectoryFor(rulePath string) string {
	stem := strings.TrimSuffix(rulePath, ".rs")
	if entries, err := os.ReadDir(stem); err == nil {
		var combined strings.Builder
		for _, entry := range entries {
			if contents, err := os.ReadFile(filepath.Join(stem, entry.Name())); err == nil {
				combined.Write(contents)
			}
		}
		return combined.String()
	}
	return ""
}

// unrecognizedKeys returns option keys that appear in a case and nowhere in the rule's own source.
//
// The discriminator is deliberately weak and that is the point: extracting a rule's real option set
// would mean parsing several unrelated Rust spellings of key lookup, and one rule in the eslint
// directory uses a hand-written TryFrom while others use serde. A key present in the implementation
// text is recognized; a key present only in the tests is not. That catches the typo case without
// needing to understand how any particular rule parses.
//
// It answers nothing about whether a recognized key is handled correctly, and it is not meant to.
func unrecognizedKeys(keys []string, ruleSource string, siblingSource string) []string {
	seen := map[string]bool{}
	var unrecognized []string

	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true

		// The test function is where a typo lives, so the implementation half is what counts. Split
		// on the test attribute rather than trying to find the function's end.
		implementation := ruleSource
		if index := strings.Index(ruleSource, "#[test]"); index >= 0 {
			implementation = ruleSource[:index]
		}
		// Both spellings, because Rust names the field in snake_case and the camelCase form appears
		// only in serde attributes and fixtures. Checking the camelCase form alone reported
		// `enforceForSwitchCase` as unrecognized while the rule reads it 56 times, which is the
		// false-positive direction and the worse one: a check that fires on correct rules trains a
		// reader to skip it.
		if strings.Contains(implementation, key) ||
			strings.Contains(implementation, snakeCase(key)) ||
			strings.Contains(siblingSource, key) ||
			strings.Contains(siblingSource, snakeCase(key)) {
			continue
		}
		unrecognized = append(unrecognized, key)
	}
	return unrecognized
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

		pass, fail := readVector(segment, "let pass"), readVector(segment, "let fail")

		// A block can pass its vectors inline as arguments rather than binding them to `let` first,
		// and reading only the `let` form reports such a block as zero pass and zero fail. That is a
		// silent zero inside the tool built to refuse silent zeros: `no-irregular-whitespace`'s
		// second block holds two regression cases and this reported none, which a porter caught only
		// because a research pass had counted them by hand.
		//
		// The inline form is the two `vec![...]` arguments of the `Tester::new` call itself, so it is
		// read from the call rather than from the text preceding it.
		if len(pass) == 0 && len(fail) == 0 {
			pass, fail = readInlineVectors(source[location[0]:end])
		}

		blocks = append(blocks, corpus{
			blockIndex:  index + 1,
			snapshotted: strings.Contains(trailing, "test_and_snapshot"),
			pass:        pass,
			fail:        fail,
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

		// A block comment too. Skipping only the line form left a leading `/* ... */` above the
		// first case splitting into a phantom entry, so `no-this-before-super` reported 40 pass
		// where it has 39. A porter caught it by transcribing the corpus by hand and reconciling,
		// which is not a check that scales.
		if current == '/' && index+1 < len(runes) && runes[index+1] == '*' {
			index += 2
			for index+1 < len(runes) && !(runes[index] == '*' && runes[index+1] == '/') {
				index++
			}
			index++
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
		// The hash count is part of the delimiter and Rust allows any number, zero included. This
		// read `r#"..."#` only, starting at index 3 and requiring a closing `"#`, so oxc's
		// zero-hash `r"..."` cases fell through to the plain-string branch below: the leading `r`
		// was dropped, the case was still counted, and the text was read as a quoted literal it is
		// not. Two of no-obj-calls's clean cases are that form, and the silence is exactly the kind
		// this tool exists to refuse.
		hashes := 0
		for 1+hashes < len(runes) && runes[1+hashes] == '#' {
			hashes++
		}
		if 1+hashes >= len(runes) || runes[1+hashes] != '"' {
			return "", false
		}
		closing := `"` + strings.Repeat("#", hashes)
		body := string(runes[1+hashes+1:])
		end := strings.Index(body, closing)
		if end < 0 {
			return "", false
		}
		return string(runes[:1+hashes+1+len([]rune(body[:end]))+len(closing)]), true
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

// snakeCase converts a camelCase option key to the Rust field spelling.
func snakeCase(name string) string {
	var built strings.Builder
	for index, character := range name {
		if character >= 'A' && character <= 'Z' {
			if index > 0 {
				built.WriteByte('_')
			}
			built.WriteRune(character - 'A' + 'a')
			continue
		}
		built.WriteRune(character)
	}
	return built.String()
}

// readRuleDirectory concatenates every Rust file under a rule that is a directory.
//
// Recursive, because the corpus can sit a level down: no-unused-vars keeps its cases in
// `tests/typescript_eslint.rs` and its option parsing in `options.rs`, and a reader taking only the
// top level sees the implementation and none of the fixtures.
func readRuleDirectory(directory string) (string, error) {
	var combined strings.Builder
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".rs") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		combined.Write(contents)
		combined.WriteString("\n")
		return nil
	})
	if err != nil {
		return "", err
	}
	if combined.Len() == 0 {
		return "", fmt.Errorf("no .rs files under %s", directory)
	}
	return combined.String(), nil
}

// countFixVector reports how many before/after pairs a rule's `let fix = vec![...]` declares.
//
// Separate from the pass and fail vectors because it asserts a different thing: those say whether a
// finding appears, this says what the repair writes. A rule can pass every fixture in both
// directions while its fix rewrites the wrong span, which is a defect this project has shipped.
func countFixVector(source string) int {
	marker := strings.Index(source, "let fix = vec!")
	if marker < 0 {
		return 0
	}
	open := strings.Index(source[marker:], "[")
	if open < 0 {
		return 0
	}
	body, ok := balancedSlice(source[marker+open:], '[', ']')
	if !ok {
		return 0
	}
	return len(readTuples(body))
}

// readInlineVectors reads the pass and fail vectors a Tester::new call passes as arguments.
//
// `Tester::new(NAME, PLUGIN, vec![...], vec![...])` rather than the usual form that binds them to
// `let pass` and `let fail` first. The two vectors are the third and fourth arguments, so this takes
// the first two `vec![` occurrences inside the call and reads each as a case list.
func readInlineVectors(call string) (pass []fixtureCase, fail []fixtureCase) {
	remaining := call
	for index := 0; index < 2; index++ {
		marker := strings.Index(remaining, "vec![")
		if marker < 0 {
			return pass, fail
		}
		body, ok := balancedSlice(remaining[marker+len("vec!"):], '[', ']')
		if !ok {
			return pass, fail
		}
		if index == 0 {
			pass = readTuples(body)
		} else {
			fail = readTuples(body)
		}
		remaining = remaining[marker+len("vec!")+len(body):]
	}
	return pass, fail
}
