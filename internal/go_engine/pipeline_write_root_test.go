package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// sharedModuleFiles are the files of the module a linkedProject reaches through a link: one gofmt would change on its
// fourth line, one whose long line only the wrap would change, and one declaring an abbreviated unexported name the
// naming rule asks to rename.
var sharedModuleFiles = map[string]string{
	"go.mod":         "module example.com/shared\n\ngo 1.24\n",
	"unformatted.go": "package shared\n\n// Total is how many there are.\nvar  Total = 2\n",
	"long.go": "package shared\n\nimport \"fmt\"\n\n// Describe says where a delivery goes.\n" +
		"func Describe(sender string, recipient string, street string, city string, weekday string) string {\n" +
		"\treturn fmt.Sprintf(\"%s sends a parcel to %s, who lives at %s in %s, and it arrives on %s\", sender, " +
		"recipient, street, city, weekday)\n}\n",
	"abbreviated.go": "package shared\n\n// cfgName is the module's name.\nvar cfgName = \"shared\"\n\n" +
		"// Name is the module's name.\nfunc Name() string {\n\treturn cfgName\n}\n",
}

// linkedProject writes a Go workspace whose go.work uses its own module and ./shared/module, where shared is a
// symbolic link to a directory beside the project, outside its root, holding sharedModuleFiles; the project's own
// module holds inside.go, which gofmt would change. It returns the project's root and the shared directory, every
// link above them resolved. Both are inside the test's own temporary directory.
func linkedProject(test *testing.T) (string, string) {
	test.Helper()
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	root, shared := filepath.Join(base, "project"), filepath.Join(base, "shared")
	files := map[string]string{
		filepath.Join(root, "go.work"):   "go 1.24\n\nuse (\n\t.\n\t./shared/module\n)\n",
		filepath.Join(root, "go.mod"):    "module example.com/project\n\ngo 1.24\n",
		filepath.Join(root, "inside.go"): "package project\n\n// Count is how many there are.\nvar  Count = 1\n",
	}
	for name, contents := range sharedModuleFiles {
		files[filepath.Join(shared, "module", name)] = contents
	}
	for name, contents := range files {
		if makeError := os.MkdirAll(filepath.Dir(name), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(name, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if linkError := os.Symlink(shared, filepath.Join(root, "shared")); linkError != nil {
		test.Fatal(linkError)
	}
	return root, shared
}

// linkedRun is what one engine run over a linkedProject wrote: its fix record, its findings, its summary and stderr.
type linkedRun struct {
	exitCode      int
	fix           contract.FixRecord
	findings      []contract.FindingRecord
	summary       contract.SummaryRecord
	standardError string
}

// runLinked runs the engine over root with extra after its contract and root, and reads back what it wrote, holding
// the stream to what the front door refuses.
func runLinked(test *testing.T, root string, extra ...string) linkedRun {
	test.Helper()
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"}, extra...)
	written := linkedRun{exitCode: run(arguments, &standardOutput, &standardError, rules.All(), nil),
		standardError: standardError.String()}
	checkStream(test, standardOutput.String(), written.exitCode)
	for line := range strings.Lines(standardOutput.String()) {
		var envelope struct {
			Kind string `json:"kind"`
		}
		if decodeError := json.Unmarshal([]byte(line), &envelope); decodeError != nil {
			test.Fatalf("%v: %s", decodeError, line)
		}
		var decodeError error
		switch envelope.Kind {
		case "fix":
			decodeError = json.Unmarshal([]byte(line), &written.fix)
			if !strings.Contains(line, `"notWritten":[`) {
				test.Errorf("the fix record writes no notWritten list: %s", line)
			}
		case "finding":
			var finding contract.FindingRecord
			decodeError = json.Unmarshal([]byte(line), &finding)
			written.findings = append(written.findings, finding)
		case "summary":
			decodeError = json.Unmarshal([]byte(line), &written.summary)
		}
		if decodeError != nil {
			test.Fatalf("%v: %s", decodeError, line)
		}
	}
	return written
}

// requireSharedUntouched fails the test unless every file of the shared module holds what linkedProject wrote.
func requireSharedUntouched(test *testing.T, shared string) {
	test.Helper()
	for name, contents := range sharedModuleFiles {
		after, readError := os.ReadFile(filepath.Join(shared, "module", name))
		if readError != nil || string(after) != contents {
			test.Errorf("%s was written through the link (%v):\n%s", name, readError, after)
		}
	}
}

// hasFinding is whether findings hold one of rule at file and line.
func hasFinding(findings []contract.FindingRecord, rule string, file string, line int) bool {
	for _, finding := range findings {
		if finding.Rule == rule && finding.File == file && (line == 0 || finding.Line == line) {
			return true
		}
	}
	return false
}

// A fix run never writes through a link to a file outside its root (#hwz114g): gofmt's rewrite, the long-line wrap and
// the rename are each withheld from the shared module the project links to, which stays byte for byte as it was. The
// fix record names each file not written with its real path, gofmt's finding is reported at its line as a check
// reports it, the long line stays the rule's finding, the rename is refused under its own reason, and the run is
// incomplete. The project's own file is still formatted. A `--no-fix` run of the same tree checks the same files,
// names no file not written, and is complete: the gap is the fix's alone.
func TestAFixNeverWritesThroughALinkOutsideTheRoot(test *testing.T) {
	test.Parallel()

	root, shared := linkedProject(test)
	fixed := runLinked(test, root)
	requireSharedUntouched(test, shared)
	inside, readError := os.ReadFile(filepath.Join(root, "inside.go"))
	if readError != nil || string(inside) != "package project\n\n// Count is how many there are.\nvar Count = 1\n" {
		test.Errorf("the project's own file was not formatted (%v):\n%s", readError, inside)
	}
	linked := filepath.Join(root, "shared", "module")
	wantNotWritten := []contract.NotWrittenFile{}
	for _, name := range []string{"abbreviated.go", "long.go", "unformatted.go"} {
		wantNotWritten = append(wantNotWritten, contract.NotWrittenFile{File: filepath.Join(linked, name),
			RealPath: filepath.Join(shared, "module", name)})
	}
	if !reflect.DeepEqual(fixed.fix.NotWritten, wantNotWritten) {
		test.Errorf("the fix record names %+v as not written, want %+v", fixed.fix.NotWritten, wantNotWritten)
	}
	if fixed.fix.RefusalsByReason[refusedOutsideRoot] != 1 {
		test.Errorf("want the rename of cfgName refused once as %q, got %v", refusedOutsideRoot,
			fixed.fix.RefusalsByReason)
	}
	for _, changed := range fixed.fix.ChangedFiles {
		if strings.HasPrefix(changed.File, linked) {
			test.Errorf("the fix record lists %s as changed", changed.File)
		}
	}
	if !hasFinding(fixed.findings, ruleRequireFormatting, filepath.Join(linked, "unformatted.go"), 4) {
		test.Errorf("no formatting finding at unformatted.go:4 in %+v", fixed.findings)
	}
	if !hasFinding(fixed.findings, consistency_no_long_line.Name, filepath.Join(linked, "long.go"), 0) {
		test.Errorf("no long-line finding in long.go in %+v", fixed.findings)
	}
	if fixed.summary.Complete || fixed.exitCode != exitFindings {
		test.Errorf("the run is complete=%t with exit %d, want incomplete and %d", fixed.summary.Complete,
			fixed.exitCode, exitFindings)
	}
	for _, name := range []string{"abbreviated.go", "long.go", "unformatted.go"} {
		note := fmt.Sprintf("%s was not written: it is reached through a link to %s, outside the root",
			filepath.Join(linked, name), filepath.Join(shared, "module", name))
		if !strings.Contains(fixed.standardError, note) {
			test.Errorf("stderr does not say %q:\n%s", note, fixed.standardError)
		}
	}

	checkRoot, checkShared := linkedProject(test)
	checked := runLinked(test, checkRoot, "--no-fix")
	requireSharedUntouched(test, checkShared)
	if len(checked.fix.NotWritten) != 0 || checked.fix.RefusalsByReason[refusedOutsideRoot] != 0 ||
		strings.Contains(checked.standardError, "was not written") {
		test.Errorf("a check names files not written: %+v\n%s", checked.fix, checked.standardError)
	}
	checkLinked := filepath.Join(checkRoot, "shared", "module")
	if !hasFinding(checked.findings, ruleRequireFormatting, filepath.Join(checkLinked, "unformatted.go"), 4) ||
		!checked.summary.Complete {
		test.Errorf("a check is complete=%t with findings %+v, want complete with unformatted.go:4 reported",
			checked.summary.Complete, checked.findings)
	}
}
