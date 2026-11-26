package doc

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/testing/recorded"
)

// widthsDirectory holds the strings whose upstream width is node's, not the bundle's under goja.
var widthsDirectory = filepath.Join("testdata", "upstream_widths")

// widthInputs is width_inputs.json: each string's name and text.
type widthInputs struct {
	About  string `json:"about"`
	Inputs []struct {
		Name string `json:"name"`
		Text string `json:"text"`
	} `json:"inputs"`
}

// widthAnswers is width_answers.json: node's width for each string, with the text it measured.
type widthAnswers struct {
	Source   string `json:"source"`
	Recorded string `json:"recorded"`
	Answers  []struct {
		Name  string `json:"name"`
		Text  string `json:"text"`
		Width int    `json:"width"`
	} `json:"answers"`
}

// TestStringWidthAgreesWithUpstreamUnderNode measures the port against the fork's own getStringWidth run
// under node, for the strings where the standalone bundle under goja answers otherwise and
// TestStringWidthAgreesWithUpstream's recording cannot hold them: a ZWJ family, which node measures as one
// emoji (9) and goja, whose regexp misses the sequence, as three (12). StringWidth answers as node does.
//
// Node's answers are recorded, so the test runs no node. They were read from the fork's
// src/utilities/get-string-width.js; to record them again, run record-string-widths.mjs, attached to
// #bahm7k3, its usage in its header.
func TestStringWidthAgreesWithUpstreamUnderNode(test *testing.T) {
	test.Parallel()
	inputs, readInputsError := recorded.ReadJson[widthInputs](filepath.Join(widthsDirectory, "width_inputs.json"))
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	answersPath := filepath.Join(widthsDirectory, "width_answers.json")
	answers, readAnswersError := recorded.ReadJson[widthAnswers](answersPath)
	if readAnswersError != nil {
		test.Fatal(readAnswersError)
	}
	if len(inputs.Inputs) == 0 {
		test.Fatal("width_inputs.json holds no strings")
	}
	inputEntries := make([]recorded.Entry, len(inputs.Inputs))
	for index, input := range inputs.Inputs {
		inputEntries[index] = recorded.Entry{Name: input.Name, Input: input.Text}
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = recorded.Entry{Name: answer.Name, Input: answer.Text}
	}
	answerIndexes, matchError := recorded.Match(inputEntries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with record-string-widths.mjs, attached to #bahm7k3", answersPath,
			matchError)
	}
	for position, input := range inputs.Inputs {
		want := answers.Answers[answerIndexes[position]].Width
		if got := StringWidth(input.Text); got != want {
			test.Errorf("%s: StringWidth(%q) = %d, upstream under node %d", input.Name, input.Text, got, want)
		}
	}
}
