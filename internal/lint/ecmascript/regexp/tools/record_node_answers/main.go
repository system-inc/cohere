// Command record_node_answers asks Node's RegExp what the regexp package's property tests hold esregexp to, and
// writes its answers as testdata, so the tests read a recording and never start Node (#3jt7qa6).
//
// Usage:
//
//	go run ./internal/lint/ecmascript/regexp/tools/record_node_answers [-check]
//
// Run from the repository root, with a Node whose process.versions.unicode is the tables' Unicode version. It
// writes two files under internal/lint/ecmascript/regexp/testdata:
//
//   - node_property_members.txt, Node's membership answers for a sample of code points per property spelling,
//     which TestUnicodePropertiesMatchNode checks the in-repo tables and the compiled escapes against;
//   - node_property_syntax.json, whether Node accepts each property escape fixture and what it matches, which
//     TestUnicodePropertySyntaxMatchesNode replays.
//
// -check regenerates and fails when either committed file disagrees. Like every other generator here, nothing
// runs it automatically: Node is required to produce the files and never to use them.
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"
	"github.com/system-inc/cohere/modules/unicode_properties"
)

// command is how a file this writes says it was made, so its header names the way to remake it.
const command = "go run ./internal/lint/ecmascript/regexp/tools/record_node_answers"

//go:embed ask.mjs
var askScript string

// syntaxFixture is one property escape pattern Node is asked to compile, then to test against every subject.
type syntaxFixture struct{ Source, Flags string }

// syntaxFixtures cover strict aliases, a property escape as a range endpoint, trailing dashes, property escapes
// beside built-in sets, modifier groups, quantifiers, an escaped backslash, and a property escape without `u`.
var syntaxFixtures = []syntaxFixture{
	{`^[\p{Script=Greek}-]$`, "u"},
	{`^[\p{Script=Greek}-A]$`, "u"},
	{`^[A-\p{Script=Greek}]$`, "u"},
	{`^[\P{ASCII}A]$`, "u"},
	{`^[^\P{ASCII}A]$`, "iu"},
	{`^[\P{Lt}\d\D\s\S\w\W]$`, "iu"},
	{`^[\p{Script=Greek}\s\d_]$`, "iu"},
	{`^(?i:\P{Script=Greek})$`, "u"},
	{`^(?i:\p{Script=Greek})(?-i:a)$`, "u"},
	{`^\P{Any}*$`, "u"},
	{`^[^\P{Any}]$`, "u"},
	{`^\p{Any}{2}$`, "u"},
	{`^\p{IsGreek}$`, "u"},
	{`^\p{script=Greek}$`, "u"},
	{`^\p{Script=greek}$`, "u"},
	{`^\p{Script=Hrkt}$`, "u"},
	{`^\p{Script=Katakana_Or_Hiragana}$`, "u"},
	{`^\p{Greek}$`, "u"},
	{`^\p{gc = Lu}$`, "u"},
	{`^\p{lowercase_letter}$`, "u"},
	{`^\p{Letter=yes}$`, "u"},
	{`^\p{Basic_Emoji}$`, "u"},
	{`^\p{Script=Greek}$`, ""},
	{`^\\p\{Script=Greek\}$`, "u"},
}

// syntaxSubjects are what every accepted fixture is tested against: ASCII and Greek letters in both cases, the
// micro sign and Greek mu, which fold together under `iu`, the characters a class's dash, `\s`, `\d` and `\w` turn
// on, two-character subjects for the quantifiers, and the escape's own text.
var syntaxSubjects = []string{
	"",
	"A",
	"a",
	"α",
	"Α",
	"µ",
	"μ",
	"-",
	"_",
	"0",
	" ",
	"\u0085",
	"\ufeff",
	"AA",
	"αa",
	"αA",
	"p{Script=Greek}",
	`\p{Script=Greek}`,
}

// alwaysAsked are asked about for every property: both ends of the code point domain, both sides of each edge of
// the surrogate block, and the micro sign and Greek mu, which are in different scripts and fold together under iu.
var alwaysAsked = []rune{0, unicode.MaxRune, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xb5, 0x3bc}

// sample is the code points Node is asked about for one property spelling. Asking about every code point of all
// 1,715 spellings is what made the test this replaces take ten seconds, so the sample is chosen to catch a wrong
// table with a few dozen per spelling:
//
//   - alwaysAsked, above;
//   - for the property's first, middle and last intervals, both edges, which are members, and the code point just
//     outside each, which is not, since the generated intervals are disjoint and merged: an edge off by one in
//     either direction disagrees at one of those four;
//   - every member of up to two case-fold groups the property splits, some members in and some out, where
//     \p under iu and \P under iu differ from \p under u, and failing a split group, one group it holds whole;
//   - two code points drawn from a hash of the spelling, which owe nothing to the table, so a table missing a stretch
//     its intervals never mention can still be caught there.
//
// A property holding every code point, Any, has no non-member to ask about. groups is foldGroups's.
func sample(name string, ranges []unicode_properties.Range, groups [][]rune) []rune {
	points := slices.Clone(alwaysAsked)
	if len(ranges) > 0 {
		for _, index := range []int{0, len(ranges) / 2, len(ranges) - 1} {
			span := ranges[index]
			for _, point := range []rune{span.Lo - 1, span.Lo, span.Hi, span.Hi + 1} {
				if point >= 0 && point <= unicode.MaxRune {
					points = append(points, point)
				}
			}
		}
	}
	covers := func(point rune) bool {
		_, found := slices.BinarySearchFunc(ranges, point, func(span unicode_properties.Range, target rune) int {
			if span.Hi < target {
				return -1
			}
			if span.Lo > target {
				return 1
			}
			return 0
		})
		return found
	}
	var split [][]rune
	var whole [][]rune
	for _, group := range groups {
		in := 0
		for _, point := range group {
			if covers(point) {
				in++
			}
		}
		if in > 0 && in < len(group) && len(split) < 2 {
			split = append(split, group)
		}
		if in == len(group) && len(whole) < 1 {
			whole = append(whole, group)
		}
	}
	if len(split) == 0 {
		split = whole
	}
	for _, group := range split {
		points = append(points, group...)
	}
	hash := fnv.New64a()
	// Ignored: a hash.Hash's Write never returns an error.
	_, _ = hash.Write([]byte(name))
	seed := hash.Sum64()
	for range 2 {
		// splitmix64's step, so the two draws are spread apart rather than neighbors.
		seed += 0x9e3779b97f4a7c15
		mixed := seed
		mixed = (mixed ^ (mixed >> 30)) * 0xbf58476d1ce4e5b9
		mixed = (mixed ^ (mixed >> 27)) * 0x94d049bb133111eb
		mixed ^= mixed >> 31
		points = append(points, rune(mixed%(unicode.MaxRune+1)))
	}
	slices.Sort(points)
	return slices.Compact(points)
}

// foldGroups is esregexp's case-fold groups under u, each sorted and in order of its first member. esregexp builds
// them from a map, so their order changes from run to run, and sample takes the first groups that qualify: in
// esregexp's order, two runs would ask about different code points and -check would never pass.
func foldGroups() [][]rune {
	groups := make([][]rune, 0, len(esregexp.CaseEquivalenceGroups(true)))
	for _, group := range esregexp.CaseEquivalenceGroups(true) {
		sorted := slices.Clone(group)
		slices.Sort(sorted)
		groups = append(groups, sorted)
	}
	slices.SortFunc(groups, func(left, right []rune) int { return int(left[0] - right[0]) })
	return groups
}

// propertyQuestion is one spelling and the code points Node is asked about.
type propertyQuestion struct {
	Name   string
	Points []rune
}

// answers is what ask.mjs writes.
type answers struct {
	Node       string
	Unicode    string
	Properties []struct {
		Name     string
		Accepted bool
		Bits     []string
	}
	Fixtures []struct {
		Accepted bool
		Matches  []bool
	}
}

// profile names ask.mjs's twelve profiles in the order it writes them.
func profile(index int) string {
	return fmt.Sprintf(
		"%s %s shape %d",
		[]string{"u", "iu"}[index/6],
		[]string{"p", "P"}[index/3%2],
		index%3,
	)
}

// derivable reports where Node's twelve answers for one code point do not follow from the three the recording
// keeps: \p under u, \p under iu and \P under iu. In ECMAScript an escape inside a class matches what it matches
// bare, a negated class matches what the class does not, and without i, \P matches what \p does not. The test
// derives the other nine from the three, so the recorder refuses to write a file where Node says otherwise.
func derivable(bits []string, pointIndex int) string {
	at := func(profileIndex int) bool { return bits[profileIndex][pointIndex] == '1' }
	for _, block := range []int{0, 3, 6, 9} {
		if at(block+1) != at(block) || at(block+2) == at(block) {
			return profile(block)
		}
	}
	if at(3) == at(0) {
		return profile(3)
	}
	return ""
}

func main() {
	check := flag.Bool("check", false, "regenerate and fail if a committed file disagrees")
	flag.Parse()

	testdata := filepath.Join("internal", "lint", "ecmascript", "regexp", "testdata")
	if _, statError := os.Stat(testdata); statError != nil {
		// Ignored: the process exits on the next line, so a failed write to stderr has nowhere left to go.
		fmt.Fprintf(os.Stderr, "run from cohere's root: %v\n", statError)
		os.Exit(1)
	}
	members, syntax, recordError := record()
	if recordError != nil {
		// Ignored: the process exits on the next line, so a failed write to stderr has nowhere left to go.
		fmt.Fprintln(os.Stderr, recordError)
		os.Exit(1)
	}
	outputs := []struct {
		path     string
		contents []byte
	}{
		{filepath.Join(testdata, "node_property_members.txt"), members},
		{filepath.Join(testdata, "node_property_syntax.json"), syntax},
	}
	stale := false
	for _, output := range outputs {
		if *check {
			committed, readError := os.ReadFile(output.path)
			if readError != nil {
				// Ignored: the process exits on the next line, so a failed write to stderr has nowhere left to go.
				fmt.Fprintln(os.Stderr, readError)
				os.Exit(1)
			}
			if !bytes.Equal(committed, output.contents) {
				// Ignored: the exit code says it whether or not this lands.
				fmt.Fprintf(
					os.Stderr,
					"%s is stale: Node answers differently. Re-run without -check and read the diff.\n",
					output.path,
				)
				stale = true
			}
			continue
		}
		if writeError := os.WriteFile(output.path, output.contents, 0o644); writeError != nil {
			// Ignored: the process exits on the next line, so a failed write to stderr has nowhere left to go.
			fmt.Fprintln(os.Stderr, writeError)
			os.Exit(1)
		}
	}
	if stale {
		os.Exit(1)
	}
}

// record asks Node and renders both files.
func record() ([]byte, []byte, error) {
	names := unicode_properties.Names()
	groups := foldGroups()
	questions := make([]propertyQuestion, len(names))
	for index, name := range names {
		property, _ := unicode_properties.Lookup(name)
		questions[index] = propertyQuestion{name, sample(name, property.Ranges, groups)}
	}
	// Node spells Unicode 17.0.0 as 17.0.
	wantUnicode := strings.TrimSuffix(unicode_properties.Version, ".0")
	input, encodeError := json.Marshal(struct {
		Unicode    string
		Properties []propertyQuestion
		Fixtures   []syntaxFixture
		Subjects   []string
	}{wantUnicode, questions, syntaxFixtures, syntaxSubjects})
	if encodeError != nil {
		return nil, nil, encodeError
	}
	node := exec.Command("node", "--input-type=module", "-e", askScript)
	node.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	node.Stderr = &stderr
	output, outputError := node.Output()
	if outputError != nil {
		return nil, nil, fmt.Errorf("node: %w: %s", outputError, stderr.String())
	}
	var answered answers
	if decodeError := json.Unmarshal(output, &answered); decodeError != nil {
		return nil, nil, fmt.Errorf("reading node's answers: %w", decodeError)
	}
	if len(answered.Properties) != len(questions) || len(answered.Fixtures) != len(syntaxFixtures) {
		return nil, nil, fmt.Errorf(
			"node answered %d spellings and %d fixtures, asked %d and %d",
			len(answered.Properties),
			len(answered.Fixtures),
			len(questions),
			len(syntaxFixtures),
		)
	}

	var members bytes.Buffer
	fmt.Fprintf(&members, `# Node's RegExp on a sample of code points for every property spelling cohere accepts.
# Do not edit by hand. Recorded by: %s
# node: %s
# unicode: %s
#
# One line per spelling: the spelling, then each code point asked about, as <hex>:<digit>. The digit adds 1 when
# \p{spelling} matches the code point under u, 2 when \p{spelling} matches it under iu, and 4 when \P{spelling}
# matches it under iu. A spelling Node refuses is followed by refused. The test derives the other nine profiles
# from these three, and the recorder checked Node's own answers for all twelve agree. How the sample is chosen
# is said at sample in the recorder.
`, command, answered.Node, answered.Unicode)
	for index, answer := range answered.Properties {
		if answer.Name != questions[index].Name {
			return nil, nil, fmt.Errorf("node answered %q where %q was asked", answer.Name, questions[index].Name)
		}
		members.WriteString(answer.Name)
		if !answer.Accepted {
			members.WriteString(" refused\n")
			continue
		}
		for pointIndex, point := range questions[index].Points {
			if why := derivable(answer.Bits, pointIndex); why != "" {
				return nil, nil, fmt.Errorf(
					"%s at U+%04X: node's %s answer does not follow from the three the recording keeps",
					answer.Name,
					point,
					why,
				)
			}
			digit := 0
			for bit, profileIndex := range []int{0, 6, 9} {
				if answer.Bits[profileIndex][pointIndex] == '1' {
					digit |= 1 << bit
				}
			}
			fmt.Fprintf(&members, " %x:%d", point, digit)
		}
		members.WriteByte('\n')
	}

	type recordedFixture struct {
		Source   string `json:"source"`
		Flags    string `json:"flags"`
		Accepted bool   `json:"accepted"`
		// Matches is a 0 or 1 per subject, in the subjects' order, and empty for a fixture Node refuses.
		Matches string `json:"matches"`
	}
	fixtures := make([]recordedFixture, len(syntaxFixtures))
	for index, fixture := range syntaxFixtures {
		var matches strings.Builder
		for _, matched := range answered.Fixtures[index].Matches {
			if matched {
				matches.WriteByte('1')
			} else {
				matches.WriteByte('0')
			}
		}
		fixtures[index] = recordedFixture{
			Source:   fixture.Source,
			Flags:    fixture.Flags,
			Accepted: answered.Fixtures[index].Accepted,
			Matches:  matches.String(),
		}
	}
	var syntax bytes.Buffer
	encoder := json.NewEncoder(&syntax)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "    ")
	if encodeError := encoder.Encode(struct {
		RecordedBy string            `json:"recordedBy"`
		Node       string            `json:"node"`
		Unicode    string            `json:"unicode"`
		Subjects   []string          `json:"subjects"`
		Fixtures   []recordedFixture `json:"fixtures"`
	}{command, answered.Node, answered.Unicode, syntaxSubjects, fixtures}); encodeError != nil {
		return nil, nil, encodeError
	}
	return members.Bytes(), syntax.Bytes(), nil
}
