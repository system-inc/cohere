package policy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSwiftFilesAreCurrent: every generated source on disk is what the generator writes from the policy
// files as they stand, so a word added to a policy file and not regenerated fails here as well as in the
// Swift tests.
func TestSwiftFilesAreCurrent(test *testing.T) {
	test.Parallel()
	files, swiftFilesError := SwiftFiles()
	if swiftFilesError != nil {
		test.Fatal(swiftFilesError)
	}
	if len(files) == 0 {
		test.Fatal("no Swift files are generated, so nothing below was checked")
	}
	for _, file := range files {
		// The test runs in policy/, one below the module root the paths start from.
		onDisk, readError := os.ReadFile(filepath.Join("..", file.Path))
		if readError != nil {
			test.Fatalf("%s: %v (run go run ./policy/tools/generate)", file.Path, readError)
		}
		if !bytes.Equal(onDisk, file.Contents) {
			test.Errorf("%s is stale (run go run ./policy/tools/generate)", file.Path)
		}
	}
}

// TestAbbreviationsFileHandsOutACopy writes into what AbbreviationsFile returns and asks again. Handing
// out the embedded slice itself would let one caller change the words every later caller reads.
func TestAbbreviationsFileHandsOutACopy(test *testing.T) {
	test.Parallel()
	handedOut := AbbreviationsFile()
	if len(handedOut) == 0 || !bytes.Equal(handedOut, abbreviationsFile) {
		test.Fatalf(
			"AbbreviationsFile is not the embedded file (%d bytes, embedded %d)",
			len(handedOut),
			len(abbreviationsFile),
		)
	}
	handedOut[0] ^= 0xff
	if !bytes.Equal(AbbreviationsFile(), abbreviationsFile) || handedOut[0] == abbreviationsFile[0] {
		test.Errorf("a write into one caller's bytes reached the embedded file")
	}
}

// TestSwiftSourceOutrunsTheText: the raw string's delimiter has more `#` than any run in the text, so a
// `"""#` or a `\#` in a policy file stays text instead of ending the string or starting an escape.
func TestSwiftSourceOutrunsTheText(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		text      string
		delimiter string
	}{
		{"{}\n", `#"""`},
		{"a \"\"\"# b\n", `##"""`},
		{"a \\## b\n", `###"""`},
	} {
		source, swiftSourceError := swiftSource("Test.json", "PolicyTest", []byte(testCase.text))
		if swiftSourceError != nil {
			test.Fatal(swiftSourceError)
		}
		opening := "static let file = " + testCase.delimiter + "\n"
		closing := "\n" + swiftStringIndentation + `"""` + strings.TrimSuffix(testCase.delimiter, `"""`) + "\n}"
		carried := swiftStringIndentation + testCase.text
		if !bytes.Contains(source, []byte(opening+carried+closing)) {
			test.Errorf("%q is not carried between %q and %q:\n%s", testCase.text, opening, closing, source)
		}
	}
}

// TestSwiftSourceIndentsTheLinesItCarries: each line of text sits at the closing delimiter's indentation,
// which Swift strips, and an empty line stays empty, so the string is the text and the file is formatted.
func TestSwiftSourceIndentsTheLinesItCarries(test *testing.T) {
	test.Parallel()
	source, swiftSourceError := swiftSource("Test.json", "PolicyTest", []byte("{\n\n    \"a\": 1\n}\n"))
	if swiftSourceError != nil {
		test.Fatal(swiftSourceError)
	}
	want := "#\"\"\"\n" + swiftStringIndentation + "{\n\n" + swiftStringIndentation + "    \"a\": 1\n" +
		swiftStringIndentation + "}\n\n" + swiftStringIndentation + "\"\"\"#\n}\n"
	if !strings.HasSuffix(string(source), want) {
		test.Errorf("the text is not carried at the delimiter's indentation:\n%s", source)
	}
}

// TestSwiftSourceRefusesACarriageReturn: a Swift multi-line string reads a carriage return as a line feed,
// so the compiled-in text would differ from the file.
func TestSwiftSourceRefusesACarriageReturn(test *testing.T) {
	test.Parallel()
	if _, swiftSourceError := swiftSource("Test.json", "PolicyTest", []byte("{}\r\n")); swiftSourceError == nil {
		test.Error("a file with a carriage return was generated")
	}
}
