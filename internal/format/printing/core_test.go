package printing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"strconv"
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

// coreDirectory holds upstream's recorded answers for TestCoreAgreesWithUpstream's toy programs.
var coreDirectory = filepath.Join("testdata", "upstream_core")

// coreProgramCount is how many programs TestCoreAgreesWithUpstream holds: the first of the old live test's
// 3,000, drawn from toyProgram at coreSeed.
const coreProgramCount = 300

// coreSeed is the old live test's seed, so the programs are the ones upstream answered.
const coreSeed = 20261002

// digestedTextLength is the length in bytes past which an answer records its program's Digest rather than
// its text: a digest is 64 bytes, so a shorter program repeated beside its name costs less and reads in
// review.
const digestedTextLength = 200

// toyCase is one toy program as upstream was given it: its AST, its comments, its text and its print width.
type toyCase struct {
	Ast      *toyNode   `json:"ast"`
	Comments []*toyNode `json:"comments"`
	Text     string     `json:"text"`
	Width    int        `json:"width"`
}

// coreAnswer is upstream's answer for one program, with what it was recorded for, the program's JSON or
// its Digest (see digestedTextLength): the recorded.Fingerprint of its formatted text and of where each
// comment attached, as attachment's JSON, or the error it raised. The test only compares each with Go's for
// equality, so the fingerprint is all of it the test needs: the answers can never be recorded again, so a
// mismatch is a regression of ours, which the failure shows in full on our side.
type coreAnswer struct {
	recorded.RecordedFor
	Formatted string `json:"formatted"`
	Attached  string `json:"attached"`
	Error     string `json:"error"`
}

// TestCoreAgreesWithUpstream holds the print core to upstream's on toy programs: the same AST and the same
// tiny printer through upstream's printAstToDoc and through PrintAstToDoc, compared on the formatted text
// and on where every comment attached.
//
// Upstream's answers are recorded, so the test runs no node and reads nothing outside this package. They
// were read from the fork's own source, src/main/ast-to-doc.js and src/main/comments, since the standalone
// bundle exports neither printAstToDoc nor printDanglingComments. The programs are the first 300 of the old
// live test's 3,000, generated here again from its seed, each with the print width the old test drew after
// it (6 + rand.Intn(30)), and each answer records the program it was recorded for, so a change to
// toyProgram fails here as answers recorded for other programs. The answers keep recorded.Fingerprint of
// each, split into core_answers_<n>.json to keep each part under 50 KB. To record them again, run
// record-printing-core.mjs, attached to #bahm7k3, its usage in its header, on the programs
// generate-toy-programs_test.go, attached there too, writes.
func TestCoreAgreesWithUpstream(test *testing.T) {
	test.Parallel()
	parts, partsError := recorded.Parts(coreDirectory, "core_answers_")
	if partsError != nil {
		test.Fatal(partsError)
	}
	var answers recorded.Answers[coreAnswer]
	for _, part := range parts {
		answersPart, readAnswersError := recorded.ReadJson[recorded.Answers[coreAnswer]](part)
		if readAnswersError != nil {
			test.Fatal(readAnswersError)
		}
		answers.Source, answers.Recorded = answersPart.Source, answersPart.Recorded
		answers.Answers = append(answers.Answers, answersPart.Answers...)
	}
	random := rand.New(rand.NewSource(coreSeed))
	cases := make([]toyCase, coreProgramCount)
	inputEntries := make([]recorded.Entry, coreProgramCount)
	for index := range cases {
		ast, comments, text := toyProgram(random)
		// Unescaped, as the recorded programs were written.
		var written bytes.Buffer
		encoder := json.NewEncoder(&written)
		encoder.SetEscapeHTML(false)
		encodeError := encoder.Encode(toyCase{Ast: ast, Comments: comments, Text: text, Width: 6 + random.Intn(30)})
		if encodeError != nil {
			test.Fatalf("program %d: %v", index, encodeError)
		}
		program := bytes.TrimSuffix(written.Bytes(), []byte("\n"))
		inputEntries[index] = recorded.Entry{Name: strconv.Itoa(index), Input: string(program)}
		if len(program) > digestedTextLength {
			inputEntries[index].Input = recorded.Digest(program)
		}
		// The Go side reads the program back from the JSON upstream was given, so neither side shares a
		// node with the generator's.
		if decodeError := json.Unmarshal(program, &cases[index]); decodeError != nil {
			test.Fatalf("program %d: %v", index, decodeError)
		}
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = answer.Entry()
	}
	answerIndexes, matchError := recorded.Match(inputEntries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with record-printing-core.mjs, attached to #bahm7k3", coreDirectory,
			matchError)
	}

	commentsTotal, upstreamErrors, textMismatches, attachMismatches := 0, 0, 0, 0
	for index, testCase := range cases {
		upstream := answers.Answers[answerIndexes[index]]
		commentsTotal += len(testCase.Comments)
		if upstream.Error != "" {
			upstreamErrors++
			if upstreamErrors <= 3 {
				test.Logf("upstream refused program %d: %s", index, upstream.Error)
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
			test.Errorf("program %d: %v\ntext %q", index, printAstToDocError, testCase.Text)
			continue
		}
		formatted := doc.Print(printed, doc.Options{PrintWidth: testCase.Width, TabWidth: 2})

		attachedGo, attachedGoError := json.Marshal(attachedSummary(testCase.Ast))
		if attachedGoError != nil {
			test.Fatalf("program %d: encoding Go's attachment: %v", index, attachedGoError)
		}
		// Go's side is shown in full on a mismatch, since upstream's can never be recorded again.
		if got := recorded.Fingerprint(string(attachedGo)); got != upstream.Attached {
			attachMismatches++
			if attachMismatches <= 3 {
				test.Errorf(
					"program %d attachment\ntext %q\nwant %s\ngot  %s, %s",
					index,
					testCase.Text,
					upstream.Attached,
					got,
					attachedGo,
				)
			}
		}
		if got := recorded.Fingerprint(formatted); got != upstream.Formatted {
			textMismatches++
			if textMismatches <= 3 {
				test.Errorf(
					"program %d output\ntext %q\nwant %s\ngot  %s, %q",
					index,
					testCase.Text,
					upstream.Formatted,
					got,
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
