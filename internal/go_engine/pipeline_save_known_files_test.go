package go_engine

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// saveRun runs an editor's save of file, under root, whose buffer is buffer, in this process, as Main runs one, but
// without format.RefuseWrites: that is process-wide, so it would refuse the writes of this package's parallel tests,
// and a save asks no write anyway. It fails the test unless the save answers with text at exit 0.
func saveRun(test *testing.T, root string, file string, buffer []byte) {
	test.Helper()
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root,
		"--fix", "--stdin-filepath", file})
	if parseError != nil {
		test.Fatal(parseError)
	}
	request.buffer = buffer
	registered := rules.All()
	ruleSettings, settingsError := loadSettings(request, registered)
	if settingsError != nil {
		test.Fatal(settingsError)
	}
	if optionsError := configureRules(ruleSettings, registered); optionsError != nil {
		test.Fatal(optionsError)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	var output bytes.Buffer
	var standardError bytes.Buffer
	engine := newPipeline(request, described, ruleSettings, registered, io.Discard, &standardError, nil)
	if exitCode := engine.runStdin(&output); exitCode != exitClean || output.Len() == 0 {
		test.Fatalf("the save of %s exited %d with %d bytes of text, want exit 0 and text:\n%s", file, exitCode,
			output.Len(), standardError.String())
	}
}

// A save looks up one file's gofmt verdict, so it must leave every other file's in the record a plain run reads
// (#h6dvf3j): after a plain run kept them all, a save of one file and then a plain run answer every file from the
// kept verdicts, as a plain run after another plain run does, and format none of them.
func TestASaveKeepsEveryOtherFilesGofmtVerdict(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "long-lines"))
	// The long lines stay, so the verdicts kept are gofmt's, which a plain run with the rule off answers from.
	if offError := turnOffLongLines(root); offError != nil {
		test.Fatal(offError)
	}
	first := plainRun(test, root)
	considered := first.fix.record.FilesConsidered
	if considered < 2 {
		test.Fatalf("the fixture holds %d Go files, and a save of one must leave another's verdict", considered)
	}
	if warm := plainRun(test, root); warm.fix.formatKnown != considered {
		test.Fatalf("a plain run after a plain run answered %d of %d files from the verdicts, so the record a save "+
			"could lose was never there", warm.fix.formatKnown, considered)
	}

	saved := filepath.Join(root, "long_lines.go")
	buffer, readError := os.ReadFile(saved)
	if readError != nil {
		test.Fatal(readError)
	}
	saveRun(test, root, saved, buffer)

	after := plainRun(test, root)
	if after.fix.formatKnown != considered || after.fix.record.FilesReformatted != 0 {
		test.Errorf("after a save of one file a plain run answered %d of %d files from the verdicts and "+
			"reformatted %d, want all answered and none reformatted", after.fix.formatKnown, considered,
			after.fix.record.FilesReformatted)
	}
}
