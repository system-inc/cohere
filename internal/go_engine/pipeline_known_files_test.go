package go_engine

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// plainRun runs the engine over root as a plain run does, writing and with the findings cache on, and returns the
// pipeline it ran, so a test reads what the fix phase held.
func plainRun(test *testing.T, root string) *pipeline {
	test.Helper()
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root})
	if parseError != nil {
		test.Fatal(parseError)
	}
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
	engine := newPipeline(request, described, ruleSettings, registered, &bytes.Buffer{}, io.Discard, nil)
	engine.run()
	if engine.session == nil || engine.fix == nil {
		test.Fatalf("the run had no findings cache (%t) or no fix phase (%t)", engine.session == nil, engine.fix == nil)
	}
	return engine
}

// goFileBytes is every Go file under root by absolute path, as it is on disk.
func goFileBytes(test *testing.T, root string) map[string][]byte {
	test.Helper()
	contents := map[string][]byte{}
	for _, relative := range goFilesOf(test, root) {
		file := filepath.Join(root, filepath.FromSlash(relative))
		read, readError := os.ReadFile(file)
		if readError != nil {
			test.Fatal(readError)
		}
		contents[file] = read
	}
	return contents
}

// A plain run over a tree no file of which moved since the last formats none of them: gofmt's verdicts kept by the
// first run answer every file, and nothing is written. A file edited between runs is formatted again, and a file the
// long-line wrap would change, once the rule is on, is wrapped, not answered from the verdict gofmt alone gave it.
func TestAPlainRunOverAnUnchangedTreeFormatsNothing(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "long-lines"))
	// The first runs leave the long lines alone, so the verdicts they keep are gofmt's only.
	if offError := turnOffLongLines(root); offError != nil {
		test.Fatal(offError)
	}

	first := plainRun(test, root)
	if first.fix.formatKnown != 0 {
		test.Errorf("the first run answered %d files from verdicts it had no cache for", first.fix.formatKnown)
	}
	before := goFileBytes(test, root)

	second := plainRun(test, root)
	considered := second.fix.record.FilesConsidered
	if considered == 0 || second.fix.formatKnown != considered {
		test.Errorf("the second run answered %d of %d files from the kept verdicts, want all", second.fix.formatKnown,
			considered)
	}
	if second.fix.record.FilesRewritten != 0 || !maps.EqualFunc(before, goFileBytes(test, root), bytes.Equal) {
		test.Errorf("the second run wrote %d files: %+v", second.fix.record.FilesRewritten,
			second.fix.record.ChangedFiles)
	}

	edited := filepath.Join(root, "long_lines.go")
	appendError := os.WriteFile(edited, append(before[edited], []byte("\nfunc  spaced() {}\n")...), 0o644)
	if appendError != nil {
		test.Fatal(appendError)
	}
	third := plainRun(test, root)
	if third.fix.record.FilesReformatted != 1 || third.fix.formatKnown != considered-1 {
		test.Errorf("after an edit the run reformatted %d files and answered %d, want 1 and %d",
			third.fix.record.FilesReformatted, third.fix.formatKnown, considered-1)
	}

	if removeError := os.Remove(filepath.Join(root, "CohereSettings.json")); removeError != nil {
		test.Fatal(removeError)
	}
	fourth := plainRun(test, root)
	wrapped := 0
	for _, changed := range fourth.fix.record.ChangedFiles {
		wrapped += changed.FixedBy[consistency_no_long_line.Name]
	}
	if wrapped == 0 || fourth.fix.wrapKnown != 0 {
		test.Errorf(
			"with the rule on the run wrapped %d lines and answered %d files from verdicts, want lines wrapped "+
				"and none answered",
			wrapped,
			fourth.fix.wrapKnown,
		)
	}
}
