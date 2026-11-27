package printing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

/*
 * A toy language for exercising the print core without a real printer: parenthesised lists of atoms, an
 * optional label on a list, and block and line comments at random gaps. The path tests walk the trees it
 * generates, so their shape and nesting come from a seed rather than by hand.
 *
 * TestCoreAgreesWithUpstream runs the same programs through upstream's real printAstToDoc, recorded, and
 * through this core, with the same tiny printer. Comment attachment is where a port drifts silently: a
 * comment on the wrong node prints in a plausible wrong place. So two things are compared: the formatted
 * text, and for every comment where it attached (leading, trailing or dangling, and on which node). The
 * second localizes a failure the first only reports.
 */

// toySource builds text and AST together, so positions are exact by construction.
type toySource struct {
	random   *rand.Rand
	text     strings.Builder
	comments []*toyNode
	counter  int
}

// gap writes one to three items: comments, newlines, blank lines, commas and spaces, so a run can put two
// comments on one line or a blank line next to a comment.
func (source *toySource) gap() {
	for count := 1 + source.random.Intn(3); count > 0; count-- {
		source.gapItem()
	}
}

func (source *toySource) gapItem() {
	switch source.random.Intn(9) {
	case 0:
		source.counter++
		start := source.text.Len()
		source.text.WriteString(fmt.Sprintf("/*b%d*/", source.counter))
		source.comments = append(source.comments, &toyNode{Kind: "Block", From: start, To: source.text.Len()})
		source.text.WriteString([]string{" ", "\n", ""}[source.random.Intn(3)])
	case 1:
		source.counter++
		start := source.text.Len()
		source.text.WriteString(fmt.Sprintf("//l%d", source.counter))
		source.comments = append(source.comments, &toyNode{Kind: "Line", From: start, To: source.text.Len()})
		source.text.WriteString("\n")
	case 2:
		source.text.WriteString("\n")
	case 3:
		source.text.WriteString("\n\n")
	case 4:
		source.text.WriteString(", ")
	case 5:
		source.text.WriteString(" ")
	}
}

func (source *toySource) node(depth int) *toyNode {
	if depth > 3 || source.random.Intn(3) == 0 {
		start := source.text.Len()
		value := []string{"a", "bb", "ccc", "identifier", "x"}[source.random.Intn(5)]
		source.text.WriteString(value)
		return &toyNode{Kind: "Atom", From: start, To: source.text.Len(), Value: value}
	}
	start := source.text.Len()
	source.text.WriteString("(")
	node := &toyNode{Kind: "List", From: start}
	// The label is a node held in its own property rather than in the children slice, so a walk of the
	// tree has to reach it both ways.
	if source.random.Intn(3) == 0 {
		labelStart := source.text.Len()
		source.text.WriteString("#tag")
		node.Label = &toyNode{Kind: "Atom", From: labelStart, To: source.text.Len(), Value: "#tag"}
		source.text.WriteString(" ")
	}
	count := source.random.Intn(4)
	for index := 0; index < count; index++ {
		source.gap()
		if index > 0 {
			source.text.WriteString(" ")
		}
		node.Children = append(node.Children, source.node(depth+1))
	}
	source.gap()
	source.text.WriteString(")")
	node.To = source.text.Len()
	return node
}

func toyProgram(random *rand.Rand) (*toyNode, []*toyNode, string) {
	source := &toySource{random: random}
	program := &toyNode{Kind: "Program"}
	count := 1 + random.Intn(3)
	for index := 0; index < count; index++ {
		source.gap()
		program.Children = append(program.Children, source.node(0))
		source.text.WriteString("\n")
	}
	source.gap()
	program.To = source.text.Len()
	return program, source.comments, source.text.String()
}

// toySettings is the toy language's per-print state, which it has none of.
type toySettings struct{}

func toyPrinter() *Printer[*toyNode, toySettings, NoArguments] {
	printer := &Printer[*toyNode, toySettings, NoArguments]{}
	printer.LocStart = func(node *toyNode) int { return node.From }
	printer.LocEnd = func(node *toyNode) int { return node.To }
	printer.VisitorKeys = func(node *toyNode) []string {
		switch node.Kind {
		case "List":
			return []string{"children", "label"}
		case "Program":
			return []string{"children"}
		}
		return nil
	}
	printer.CanAttachComment = func(
		node *toyNode,
		_ []*toyNode,
	) bool {
		return node.Kind == "Atom" || node.Kind == "List"
	}
	printer.IsBlockComment = func(comment *toyNode) bool { return comment.Kind == "Block" }
	// A handler, so the hooks are exercised and placement is observable: an end-of-line block comment
	// inside a list dangles on that list. Without one, a mutation that misclassified end-of-line comments
	// survived, because default attachment sends both placements to the same node; the JavaScript
	// handlers branch on placement, so the toy has to as well.
	printer.HandleComments.EndOfLine = func(context *CommentContext[*toyNode, toySettings, NoArguments]) bool {
		if context.HasEnclosing && context.Enclosing.Kind == "List" && context.Comment.Kind == "Block" {
			AddDanglingComment(context.Enclosing, context.Comment, "")
			return true
		}
		return false
	}
	printer.PrintComment = func(
		path *AstPath[*toyNode, toySettings],
		options *Options[*toyNode, toySettings, NoArguments],
	) doc.Doc {
		comment, _ := path.Node()
		return doc.Text(options.OriginalText[comment.From:comment.To])
	}
	printer.Print = func(
		path *AstPath[*toyNode, toySettings],
		options *Options[*toyNode, toySettings, NoArguments],
		print PrintFunction[NoArguments],
		_ *NoArguments,
	) doc.Doc {
		node, _ := path.Node()
		children := Map(path, func(*AstPath[*toyNode, toySettings], int) doc.Doc { return print("", nil) }, "children")
		switch node.Kind {
		case "Program":
			return doc.Concat{
				doc.Join(doc.Hardline, children),
				PrintDanglingComments(path, options, DanglingOptions[*toyNode]{}),
				doc.Hardline,
			}
		case "List":
			var label doc.Doc = doc.Text("")
			if node.Label != nil {
				label = doc.Concat{print("label", nil), doc.Text(" ")}
			}
			if len(children) == 0 {
				return doc.Concat{
					doc.Text("("),
					label,
					PrintDanglingComments(path, options, DanglingOptions[*toyNode]{}),
					doc.Text(")"),
				}
			}
			return doc.NewGroup(doc.Concat{
				doc.Text("("), label,
				doc.NewIndent(doc.Concat{doc.Softline, doc.Join(doc.LineDoc, children)}),
				PrintDanglingComments(path, options, DanglingOptions[*toyNode]{}),
				doc.Softline,
				doc.Text(")"),
			}, doc.GroupOptions{})
		default:
			return doc.Text(node.Value)
		}
	}
	return printer
}

func attachedSummary(root *toyNode) []attachment {
	// Empty, not nil: a nil slice encodes as null and upstream's empty array as [], and every comment-free
	// program read as a mismatch until this said so.
	attached := []attachment{}
	var walk func(*toyNode)
	walk = func(node *toyNode) {
		for _, comment := range node.Comments {
			kind := "dangling"
			if comment.Leading {
				kind = "leading"
			} else if comment.Trailing {
				kind = "trailing"
			}
			attached = append(attached, attachment{
				CommentStart: comment.From,
				Kind:         kind,
				NodeType:     node.Kind,
				NodeStart:    node.From,
			})
		}
		if node.Label != nil {
			walk(node.Label)
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
	for left := 1; left < len(attached); left++ {
		for right := left; right > 0 && attached[right].CommentStart < attached[right-1].CommentStart; right-- {
			attached[right], attached[right-1] = attached[right-1], attached[right]
		}
	}
	return attached
}

// coreDirectory holds TestCoreAgreesWithUpstream's toy programs and upstream's recorded answers for them.
var coreDirectory = filepath.Join("testdata", "upstream_core")

// toyCase is one toy program as the inputs hold it: its AST, its comments, its text and its print width.
type toyCase struct {
	Ast      *toyNode   `json:"ast"`
	Comments []*toyNode `json:"comments"`
	Text     string     `json:"text"`
	Width    int        `json:"width"`
}

// coreInputs is core_inputs.json.gz: each program kept as the JSON text it is written in, since its digest
// is what its answer was recorded for.
type coreInputs struct {
	About  string `json:"about"`
	Inputs []struct {
		Name string          `json:"name"`
		Case json.RawMessage `json:"case"`
	} `json:"inputs"`
}

// coreAnswers is core_answers.json.gz: for each program, upstream's formatted text and where each comment
// attached, or the error it raised.
type coreAnswers struct {
	Source   string `json:"source"`
	Recorded string `json:"recorded"`
	Answers  []struct {
		Name       string       `json:"name"`
		CaseSha256 string       `json:"caseSha256"`
		Formatted  string       `json:"formatted"`
		Attached   []attachment `json:"attached"`
		Error      string       `json:"error"`
	} `json:"answers"`
}

// TestCoreAgreesWithUpstream holds the print core to upstream's on toy programs: the same AST and the same
// tiny printer through upstream's printAstToDoc and through PrintAstToDoc, compared on the formatted text
// and on where every comment attached.
//
// Upstream's answers are recorded, so the test runs no node and reads nothing outside this package. They
// were read from the fork's own source, src/main/ast-to-doc.js and src/main/comments, since the standalone
// bundle exports neither printAstToDoc nor printDanglingComments. The programs are the first 300 of the old
// live test's 3,000, at its seed (core_inputs.json.gz says how they were made). To record them again, run
// record-printing-core.mjs, attached to #bahm7k3, its usage in its header; an answer recorded for another
// program fails here (see package recorded).
func TestCoreAgreesWithUpstream(test *testing.T) {
	test.Parallel()
	inputs, readInputsError := recorded.ReadJson[coreInputs](filepath.Join(coreDirectory, "core_inputs.json.gz"))
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	answersPath := filepath.Join(coreDirectory, "core_answers.json.gz")
	answers, readAnswersError := recorded.ReadJson[coreAnswers](answersPath)
	if readAnswersError != nil {
		test.Fatal(readAnswersError)
	}
	if len(inputs.Inputs) == 0 {
		test.Fatal("core_inputs.json.gz holds no programs")
	}
	cases := make([]toyCase, len(inputs.Inputs))
	inputEntries := make([]recorded.Entry, len(inputs.Inputs))
	for index, input := range inputs.Inputs {
		var compact bytes.Buffer
		if compactError := json.Compact(&compact, input.Case); compactError != nil {
			test.Fatalf("program %s: %v", input.Name, compactError)
		}
		inputEntries[index] = recorded.Entry{Name: input.Name, Input: recorded.Digest(compact.Bytes())}
		if decodeError := json.Unmarshal(input.Case, &cases[index]); decodeError != nil {
			test.Fatalf("program %s: %v", input.Name, decodeError)
		}
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = recorded.Entry{Name: answer.Name, Input: answer.CaseSha256}
	}
	answerIndexes, matchError := recorded.Match(inputEntries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with record-printing-core.mjs, attached to #bahm7k3", answersPath,
			matchError)
	}

	commentsTotal, upstreamErrors, textMismatches, attachMismatches := 0, 0, 0, 0
	for index, testCase := range cases {
		upstream := answers.Answers[answerIndexes[index]]
		commentsTotal += len(testCase.Comments)
		if upstream.Error != "" {
			upstreamErrors++
			if upstreamErrors <= 3 {
				test.Logf("upstream refused program %s: %s", inputs.Inputs[index].Name, upstream.Error)
			}
			continue
		}
		options := &Options[*toyNode, toySettings, NoArguments]{
			Printer:                    toyPrinter(),
			OriginalText:               testCase.Text,
			EmbeddedLanguageFormatting: "off",
		}
		printed, printAstToDocError := PrintAstToDoc(testCase.Ast, testCase.Comments, options)
		if printAstToDocError != nil {
			test.Errorf("program %s: %v\ntext %q", inputs.Inputs[index].Name, printAstToDocError, testCase.Text)
			continue
		}
		formatted := doc.Print(printed, doc.Options{PrintWidth: testCase.Width, TabWidth: 2})

		attachedGo, attachedGoError := json.Marshal(attachedSummary(testCase.Ast))
		if attachedGoError != nil {
			test.Fatalf("program %s: encoding Go's attachment: %v", inputs.Inputs[index].Name, attachedGoError)
		}
		// Empty, not null, as attachedSummary's is, for a program with no comments.
		upstreamAttached := upstream.Attached
		if upstreamAttached == nil {
			upstreamAttached = []attachment{}
		}
		attachedUpstream, attachedUpstreamError := json.Marshal(upstreamAttached)
		if attachedUpstreamError != nil {
			test.Fatalf("program %s: encoding upstream's attachment: %v", inputs.Inputs[index].Name,
				attachedUpstreamError)
		}
		if string(attachedGo) != string(attachedUpstream) {
			attachMismatches++
			if attachMismatches <= 3 {
				test.Errorf(
					"program %s attachment\ntext %q\nwant %s\ngot  %s",
					inputs.Inputs[index].Name,
					testCase.Text,
					attachedUpstream,
					attachedGo,
				)
			}
		}
		if formatted != upstream.Formatted {
			textMismatches++
			if textMismatches <= 3 {
				test.Errorf(
					"program %s output\ntext %q\nwant %q\ngot  %q",
					inputs.Inputs[index].Name,
					testCase.Text,
					upstream.Formatted,
					formatted,
				)
			}
		}
	}
	test.Logf(
		"%d programs against %s, recorded %s: %d comments, %d upstream refused, %d attachment mismatches, %d "+
			"output mismatches",
		len(cases),
		answers.Source,
		answers.Recorded,
		commentsTotal,
		upstreamErrors,
		attachMismatches,
		textMismatches,
	)
	if upstreamErrors > len(cases)/10 {
		test.Errorf("upstream refused %d of %d programs, so the comparison is thinner than it looks", upstreamErrors,
			len(cases))
	}
	if commentsTotal == 0 {
		test.Error("no program held a comment, so attachment went untested")
	}
}
