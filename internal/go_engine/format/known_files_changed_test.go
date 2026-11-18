package format

import (
	"os"
	"path/filepath"
	"testing"
)

// The verdicts are kept only when they changed (#b2t9xmf): a run that learns a verdict, or no longer reads one it was
// handed, has changed them, and a run that reads every verdict it was handed and learns none has not, so its caller
// writes nothing. Encode can't say which, since gob writes a map in no fixed order.
func TestTheVerdictsChangeOnlyWhenARunLearnsOrDropsOne(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	clean := filepath.Join(directory, "clean.go")
	generated := filepath.Join(directory, "generated.go")
	for file, source := range map[string]string{clean: formattedSource, generated: generatedSource} {
		if writeError := os.WriteFile(file, []byte(source), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", file, writeError)
		}
	}
	files := []string{clean, generated}

	first := DecodeKnownFiles(nil)
	Run(files, Options{Known: first})
	if !first.Changed() {
		test.Fatalf("want a run that learned two verdicts changed")
	}
	encoded, encodeError := first.Encode()
	if encodeError != nil {
		test.Fatalf("encoding the verdicts: %v", encodeError)
	}

	again := DecodeKnownFiles(encoded)
	if answered := Run(files, Options{Known: again}); answered.Known != 2 {
		test.Fatalf("want both files known, got %+v", answered)
	}
	if again.Changed() {
		test.Fatalf("want a run that read every verdict it was handed and learned none unchanged")
	}

	fewer := DecodeKnownFiles(encoded)
	Run([]string{clean}, Options{Known: fewer})
	if !fewer.Changed() {
		test.Fatalf("want a run that no longer read the generated file's verdict changed, so it is dropped")
	}

	if writeError := os.WriteFile(clean, []byte(reformattedSource), 0o644); writeError != nil {
		test.Fatalf("editing %s: %v", clean, writeError)
	}
	edited := DecodeKnownFiles(encoded)
	Run(files, Options{Known: edited})
	if !edited.Changed() {
		test.Fatalf("want a run over an edited file changed")
	}
}
