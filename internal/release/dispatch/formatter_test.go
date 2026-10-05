package dispatch

import (
	"os"
	"strings"
	"testing"
)

// The formatter's identity keys the format record, so it must move with everything that can change
// what the formatter prints and with nothing else. A missed input keeps a record across a printer
// change, which skips files the new printer would rewrite; an extra one throws the record away on a
// lint commit, which is the cost this identity exists to remove.

// TestFormatterInputsCoverWhatCanChangeTheOutput reads the real module's inputs and checks each class
// is there: the printers, the options they resolve, the typescript-go parser they parse with, and the
// pass loop, and that no lint rule is.
func TestFormatterInputsCoverWhatCanChangeTheOutput(t *testing.T) {
	t.Parallel()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	module, found := walkUpForModule(workingDirectory)
	if !found {
		t.Fatal("this test runs inside the cohere module and could not find it")
	}

	inputs, err := formatterInputs(module, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, input := range inputs {
		names[input.Name] = true
		if strings.Contains(input.Name, "/internal/lint/rules/") {
			t.Errorf("a lint rule is a formatter input, so a rule commit would throw the record away: %s", input.Name)
		}
		if len(input.Sum) != 64 {
			t.Errorf("%s has no content hash: %q", input.Name, input.Sum)
		}
	}
	for _, class := range []struct{ what, contains string }{
		{"the printers", "github.com/system-inc/cohere/internal/format/native/"},
		{"the options they resolve", "github.com/system-inc/cohere/internal/format/formatoptions/"},
		{"the parser they parse with", "github.com/microsoft/TypeScript/tsc/"},
		{"the pass loop", formatterPassLoop},
	} {
		present := false
		for name := range names {
			present = present || strings.Contains(name, class.contains)
		}
		if !present {
			t.Errorf("%s (%s) are not among the formatter's inputs", class.what, class.contains)
		}
	}
}

func TestFormatterIdentityMovesWithEveryInputAndNotWithOrder(t *testing.T) {
	t.Parallel()

	inputs := []formatterInput{
		{Name: "github.com/system-inc/cohere/internal/format/native/native.go", Sum: strings.Repeat("a", 64)},
		{Name: "github.com/microsoft/TypeScript/tsc/internal/parser/parser.go", Sum: strings.Repeat("b", 64)},
		{Name: formatterPassLoop, Sum: strings.Repeat("c", 64)},
	}
	base := hashFormatterInputs("go1.27.0 darwin arm64", inputs)

	reversed := []formatterInput{inputs[2], inputs[1], inputs[0]}
	if hashFormatterInputs("go1.27.0 darwin arm64", reversed) != base {
		t.Fatal("the same inputs in another order hashed differently, so go list's order would discard the record")
	}
	if hashFormatterInputs("go1.28.0 darwin arm64", inputs) == base {
		t.Fatal("a toolchain change did not move the identity")
	}
	for index := range inputs {
		changed := append([]formatterInput(nil), inputs...)
		changed[index].Sum = strings.Repeat("d", 64)
		if hashFormatterInputs("go1.27.0 darwin arm64", changed) == base {
			t.Errorf("an edit to %s did not move the identity", inputs[index].Name)
		}
		renamed := append([]formatterInput(nil), inputs...)
		renamed[index].Name += ".moved"
		if hashFormatterInputs("go1.27.0 darwin arm64", renamed) == base {
			t.Errorf("moving %s did not move the identity", inputs[index].Name)
		}
	}
	if hashFormatterInputs("go1.27.0 darwin arm64", inputs[:2]) == base {
		t.Fatal("dropping an input did not move the identity")
	}
}
