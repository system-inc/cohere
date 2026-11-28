package doc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/recorded"
)

// showDoc is a canonical serialization of a doc's structure, so a comparison of two docs catches a pair that
// prints the same but builds a different tree.
func showDoc(document Doc) string {
	quote := func(text string) string {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		// Ignored: a string always has a JSON form, and a bytes.Buffer never fails a write.
		_ = encoder.Encode(text)
		return strings.TrimSuffix(buffer.String(), "\n")
	}
	list := func(parts []Doc) string {
		shown := make([]string, len(parts))
		for index, part := range parts {
			shown[index] = showDoc(part)
		}
		return strings.Join(shown, ",")
	}
	idName := func(id *GroupId) string {
		if id == nil {
			return ""
		}
		return id.name
	}
	switch typed := document.(type) {
	case nil:
		return "undefined"
	case Text:
		return quote(string(typed))
	case *Group:
		result := "group(" + idName(typed.Id) + "," + fmt.Sprint(typed.Break) + "," + showDoc(typed.Contents)
		if typed.ExpandedStates != nil {
			result += ",states[" + list(typed.ExpandedStates) + "]"
		}
		return result + ")"
	case *Fill:
		return "fill[" + list(typed.Parts) + "]"
	case *IfBreak:
		return "ifBreak(" +
			idName(typed.GroupId) +
			"," +
			showDoc(typed.BreakContents) +
			"," +
			showDoc(typed.FlatContents) +
			")"
	case *Indent:
		return "indent(" + showDoc(typed.Contents) + ")"
	case *Align:
		var argument string
		switch typed.Kind {
		case AlignWidth:
			argument = fmt.Sprint(typed.Width)
		case AlignString:
			argument = quote(typed.String)
		case AlignRoot:
			argument = "markRoot"
		case AlignDedentToRoot:
			argument = "root"
		}
		return "align(" + argument + "," + showDoc(typed.Contents) + ")"
	case *IndentIfBreak:
		return "indentIfBreak(" +
			idName(typed.GroupId) +
			"," +
			fmt.Sprint(typed.Negate) +
			"," +
			showDoc(typed.Contents) +
			")"
	case *LineSuffix:
		return "lineSuffix(" + showDoc(typed.Contents) + ")"
	case lineSuffixBoundaryDoc:
		return "lineSuffixBoundary"
	case *Line:
		return "line(" + fmt.Sprint(typed.Soft) + "," + fmt.Sprint(typed.Hard) + "," + fmt.Sprint(typed.Literal) + ")"
	case breakParentDoc:
		return "breakParent"
	case trimDoc:
		return "trim"
	case *Label:
		return "label(" + typed.Label + "," + showDoc(typed.Contents) + ")"
	}
	if parts, isArray := Parts(document); isArray {
		return "[" + list(parts) + "]"
	}
	panic(fmt.Sprintf("unknown doc %T", document))
}

// utilitiesDirectory holds upstream's recorded answers for TestUtilitiesAgreeWithUpstream's specs.
var utilitiesDirectory = filepath.Join("testdata", "upstream_utilities")

// utilitiesSpecCount is how many specs TestUtilitiesAgreeWithUpstream holds: the first of the old live test's
// 3,000, drawn from specGenerator at utilitiesSeed.
const utilitiesSpecCount = 350

// utilitiesSeed is the old live test's seed, so the specs are the ones upstream answered.
const utilitiesSeed = 20261002

// digestedTextLength is the length in bytes past which an answer records its spec's Digest rather than its
// text: a digest is 64 bytes, so a shorter spec repeated beside its name costs less and reads in review.
const digestedTextLength = 200

// writtenSpec is a spec as its answer was recorded for it: the same fields in the same order, each left out
// when it is zero, so the JSON is what the recorder read.
type writtenSpec struct {
	Kind      string         `json:"Kind"`
	Text      string         `json:"Text,omitempty"`
	Children  []*writtenSpec `json:"Children,omitempty"`
	Number    int            `json:"Number,omitempty"`
	Flag      bool           `json:"Flag,omitempty"`
	Id        string         `json:"Id,omitempty"`
	Reference int            `json:"Reference,omitempty"`
}

// writeSpec is a spec as writtenSpec holds it.
func writeSpec(node *spec) *writtenSpec {
	written := &writtenSpec{
		Kind:      node.Kind,
		Text:      node.Text,
		Number:    node.Number,
		Flag:      node.Flag,
		Id:        node.Id,
		Reference: node.Reference,
	}
	for _, child := range node.Children {
		written.Children = append(written.Children, writeSpec(child))
	}
	return written
}

// utilitiesOutcome is upstream's answer for one spec, with what it was recorded for, the spec's JSON or its
// Digest (see digestedTextLength): the three booleans, and the recorded.Fingerprint of each utility's
// result, a doc shown in showDoc's serialization and, for cleanDoc and stripTrailingHardline, printed at
// printWidth 20 and tabWidth 2; or the error the spec raised. The test only compares each doc with Go's for
// equality, so the fingerprint is all of it the test needs: the answers can never be recorded again, so a
// mismatch is a regression of ours, which the failure shows in full on our side.
type utilitiesOutcome struct {
	recorded.RecordedFor
	WillBreak    bool   `json:"willBreak"`
	CanBreak     bool   `json:"canBreak"`
	IsEmpty      bool   `json:"isEmpty"`
	Clean        string `json:"clean"`
	CleanPrinted string `json:"cleanPrinted"`
	Strip        string `json:"strip"`
	StripPrinted string `json:"stripPrinted"`
	Replaced     string `json:"replaced"`
	Removed      string `json:"removed"`
	Error        string `json:"error"`
}

// TestUtilitiesAgreeWithUpstream holds the doc utilities to upstream's, src/document/utilities in the
// fork: the same random specs printer_test.go generates are built on both sides, each utility is applied,
// and the results compared: the booleans directly, and the docs both structurally (showDoc, so a transform
// that prints the same but builds a different tree is still caught) and by printing them.
//
// Upstream's answers are recorded, so the test runs no node and reads nothing outside this package. They
// were read from the fork's own source, src/document/public.js and src/document/utilities/index.js, since
// the standalone bundle exports neither cleanDoc nor isEmptyDoc. The specs are the first 350 of the old
// live test's 3,000, generated here again from its seed, and each answer records the spec it was recorded
// for, so a change to specGenerator fails here as answers recorded for other specs. The answers keep each
// doc's recorded.Fingerprint, split into utilities_answers_<n>.json to keep each part under 50 KB. To
// record them again, run record-doc-utilities.mjs, attached to #bahm7k3, its usage in its header, on the
// specs generate-utility-specs_test.go, attached there too, writes.
func TestUtilitiesAgreeWithUpstream(test *testing.T) {
	test.Parallel()
	parts, partsError := recorded.Parts(utilitiesDirectory, "utilities_answers_")
	if partsError != nil {
		test.Fatal(partsError)
	}
	var answers recorded.Answers[utilitiesOutcome]
	for _, part := range parts {
		answersPart, readAnswersError := recorded.ReadJson[recorded.Answers[utilitiesOutcome]](part)
		if readAnswersError != nil {
			test.Fatal(readAnswersError)
		}
		answers.Source, answers.Recorded = answersPart.Source, answersPart.Recorded
		answers.Answers = append(answers.Answers, answersPart.Answers...)
	}
	random := rand.New(rand.NewSource(utilitiesSeed))
	specs := make([]*spec, utilitiesSpecCount)
	specTexts := make([]string, utilitiesSpecCount)
	inputEntries := make([]recorded.Entry, utilitiesSpecCount)
	for index := range specs {
		generator := &specGenerator{random: random}
		specs[index] = generator.node()
		// Unescaped, as the recorded specs were written: json.Marshal would write alignString's "> " as "\u003e ".
		var written bytes.Buffer
		encoder := json.NewEncoder(&written)
		encoder.SetEscapeHTML(false)
		if encodeError := encoder.Encode(writeSpec(specs[index])); encodeError != nil {
			test.Fatalf("spec %d: %v", index, encodeError)
		}
		specTexts[index] = strings.TrimSuffix(written.String(), "\n")
		inputEntries[index] = recorded.Entry{Name: strconv.Itoa(index), Input: specTexts[index]}
		if len(specTexts[index]) > digestedTextLength {
			inputEntries[index].Input = recorded.Digest([]byte(specTexts[index]))
		}
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = answer.Entry()
	}
	answerIndexes, matchError := recorded.Match(inputEntries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with record-doc-utilities.mjs, attached to #bahm7k3",
			utilitiesDirectory, matchError)
	}

	print := func(document Doc) (result string) {
		defer func() {
			if recovered := recover(); recovered != nil {
				result = fmt.Sprint("error: ", recovered)
			}
		}()
		return Print(document, Options{PrintWidth: 20, TabWidth: 2})
	}

	mismatches, upstreamErrors := 0, 0
	report := func(index int, what string, want string, got string) {
		mismatches++
		if mismatches <= 8 {
			test.Errorf("spec %d %s\nspec %s\nwant %v\ngot  %v", index, what, specTexts[index], want, got)
		}
	}
	// compare holds Go's result to upstream's fingerprint of it, and says whether they agree; a mismatch shows
	// Go's result in full, since upstream's can never be recorded again.
	compare := func(index int, what string, want string, got string) bool {
		if gotFingerprint := recorded.Fingerprint(got); gotFingerprint != want {
			report(index, what, want, fmt.Sprintf("%s, %q", gotFingerprint, got))
			return false
		}
		return true
	}
	for index, each := range specs {
		want := answers.Answers[answerIndexes[index]]
		if want.Error != "" {
			upstreamErrors++
			continue
		}
		if got := WillBreak(buildSpec(each)); got != want.WillBreak {
			report(index, "willBreak", fmt.Sprint(want.WillBreak), fmt.Sprint(got))
		}
		if got := CanBreak(buildSpec(each)); got != want.CanBreak {
			report(index, "canBreak", fmt.Sprint(want.CanBreak), fmt.Sprint(got))
		}
		if got := IsEmptyDoc(buildSpec(each)); got != want.IsEmpty {
			report(index, "isEmptyDoc", fmt.Sprint(want.IsEmpty), fmt.Sprint(got))
		}
		cleaned := CleanDoc(buildSpec(each))
		if compare(index, "cleanDoc", want.Clean, showDoc(cleaned)) {
			compare(index, "cleanDoc printed", want.CleanPrinted, print(cleaned))
		}
		stripped := StripTrailingHardline(buildSpec(each))
		if compare(index, "stripTrailingHardline", want.Strip, showDoc(stripped)) {
			compare(index, "stripTrailingHardline printed", want.StripPrinted, print(stripped))
		}
		compare(index, "replaceEndOfLine", want.Replaced, showDoc(ReplaceEndOfLine(buildSpec(each), nil)))
		compare(index, "removeLines", want.Removed, showDoc(RemoveLines(buildSpec(each))))
	}
	test.Logf("%d docs against %s, recorded %s: %d upstream refused, %d mismatches", len(specs), answers.Source,
		answers.Recorded, upstreamErrors, mismatches)
	if upstreamErrors > len(specs)/10 {
		test.Errorf("upstream refused %d of %d docs; the comparison is thinner than it looks", upstreamErrors,
			len(specs))
	}
}
