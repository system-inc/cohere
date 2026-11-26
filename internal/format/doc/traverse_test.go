package doc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
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

// utilitiesDirectory holds TestUtilitiesAgreeWithUpstream's specs and upstream's recorded answers for them.
var utilitiesDirectory = filepath.Join("testdata", "upstream_utilities")

// utilitiesInputs is utilities_inputs.json.gz: random doc specs, each kept as the JSON text it is written
// in, since its digest is what its answer was recorded for.
type utilitiesInputs struct {
	About  string `json:"about"`
	Inputs []struct {
		Name string          `json:"name"`
		Spec json.RawMessage `json:"spec"`
	} `json:"inputs"`
}

// utilitiesOutcome is upstream's answer for one spec: each utility's result, a doc shown in showDoc's
// serialization and, for cleanDoc and stripTrailingHardline, printed at printWidth 20 and tabWidth 2; or the
// error the spec raised.
type utilitiesOutcome struct {
	Name         string `json:"name"`
	SpecSha256   string `json:"specSha256"`
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

// utilitiesAnswers is utilities_answers.json.gz.
type utilitiesAnswers struct {
	Source   string             `json:"source"`
	Recorded string             `json:"recorded"`
	Answers  []utilitiesOutcome `json:"answers"`
}

// TestUtilitiesAgreeWithUpstream holds the doc utilities to upstream's, src/document/utilities in the
// fork: the same random specs printer_test.go generates are built on both sides, each utility is applied,
// and the results compared: the booleans directly, and the docs both structurally (showDoc, so a transform
// that prints the same but builds a different tree is still caught) and by printing them.
//
// Upstream's answers are recorded, so the test runs no node and reads nothing outside this package. They
// were read from the fork's own source, src/document/public.js and src/document/utilities/index.js, since
// the standalone bundle exports neither cleanDoc nor isEmptyDoc. The specs are the first 350 of the old
// live test's 3,000, at its seed (utilities_inputs.json.gz says how they were made). To record them again,
// run record-doc-utilities.mjs, attached to #bahm7k3, its usage in its header; an answer recorded for
// another spec fails here (see package recorded).
func TestUtilitiesAgreeWithUpstream(test *testing.T) {
	test.Parallel()
	inputs, readInputsError := recorded.ReadJson[utilitiesInputs](
		filepath.Join(utilitiesDirectory, "utilities_inputs.json.gz"),
	)
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	answersPath := filepath.Join(utilitiesDirectory, "utilities_answers.json.gz")
	answers, readAnswersError := recorded.ReadJson[utilitiesAnswers](answersPath)
	if readAnswersError != nil {
		test.Fatal(readAnswersError)
	}
	if len(inputs.Inputs) == 0 {
		test.Fatal("utilities_inputs.json.gz holds no specs")
	}
	specs := make([]*spec, len(inputs.Inputs))
	inputEntries := make([]recorded.Entry, len(inputs.Inputs))
	for index, input := range inputs.Inputs {
		var compact bytes.Buffer
		if compactError := json.Compact(&compact, input.Spec); compactError != nil {
			test.Fatalf("spec %s: %v", input.Name, compactError)
		}
		inputEntries[index] = recorded.Entry{Name: input.Name, Input: recorded.Digest(compact.Bytes())}
		if decodeError := json.Unmarshal(input.Spec, &specs[index]); decodeError != nil {
			test.Fatalf("spec %s: %v", input.Name, decodeError)
		}
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = recorded.Entry{Name: answer.Name, Input: answer.SpecSha256}
	}
	answerIndexes, matchError := recorded.Match(inputEntries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with record-doc-utilities.mjs, attached to #bahm7k3", answersPath,
			matchError)
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
			test.Errorf("spec %s %s\nspec %s\nwant %v\ngot  %v", inputs.Inputs[index].Name, what,
				inputs.Inputs[index].Spec, want, got)
		}
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
		if got := showDoc(cleaned); got != want.Clean {
			report(index, "cleanDoc", want.Clean, got)
		} else if got := print(cleaned); got != want.CleanPrinted {
			report(index, "cleanDoc printed", want.CleanPrinted, got)
		}
		stripped := StripTrailingHardline(buildSpec(each))
		if got := showDoc(stripped); got != want.Strip {
			report(index, "stripTrailingHardline", want.Strip, got)
		} else if got := print(stripped); got != want.StripPrinted {
			report(index, "stripTrailingHardline printed", want.StripPrinted, got)
		}
		if got := showDoc(ReplaceEndOfLine(buildSpec(each), nil)); got != want.Replaced {
			report(index, "replaceEndOfLine", want.Replaced, got)
		}
		if got := showDoc(RemoveLines(buildSpec(each))); got != want.Removed {
			report(index, "removeLines", want.Removed, got)
		}
	}
	test.Logf("%d docs against %s, recorded %s: %d upstream refused, %d mismatches", len(specs), answers.Source,
		answers.Recorded, upstreamErrors, mismatches)
	if upstreamErrors > len(specs)/10 {
		test.Errorf("upstream refused %d of %d docs; the comparison is thinner than it looks", upstreamErrors,
			len(specs))
	}
}
