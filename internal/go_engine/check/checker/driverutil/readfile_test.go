//cohere:file-ignore consistency-require-separated-folder-words x/tools' driverutil, the package this one copies
package driverutil_test

import (
	"go/parser"
	"go/token"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/checker/driverutil"
)

// passOfFiles is a pass over count parsed files named file0.go and on, with one other file and one ignored file, and
// the names of its parsed files in order.
func passOfFiles(test *testing.T, count int) (*analysis.Pass, []string) {
	test.Helper()
	fileSet := token.NewFileSet()
	pass := &analysis.Pass{Fset: fileSet, OtherFiles: []string{"other.s"}, IgnoredFiles: []string{"ignored.go"}}
	var names []string
	for index := range count {
		name := "file" + strconv.Itoa(index) + ".go"
		file, parseError := parser.ParseFile(fileSet, name, "package files\n", parser.ParseComments)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", name, parseError)
		}
		pass.Files = append(pass.Files, file)
		names = append(names, name)
	}
	return pass, names
}

// readName is a ReadFileFunc that hands back the name it was asked for, so a test sees which read went through.
func readName(filename string) ([]byte, error) {
	return []byte(filename), nil
}

// TestCheckedReadFileReadsThePassFileNamesOnce holds CheckedReadFile to CheckReadable's answer for every name, and
// to reading the names of pass.Files once for the pass: once a read has needed them, a pass whose Files are taken
// away still reads every name it held, where a scan of pass.Files on each call, as CheckReadable makes, would refuse
// them. A read of an other or ignored file never needs the names, so a pass whose Files change after one reads the
// new ones.
func TestCheckedReadFileReadsThePassFileNamesOnce(test *testing.T) {
	test.Parallel()
	pass, names := passOfFiles(test, 8)
	readFile := driverutil.CheckedReadFile(pass, readName)
	for _, name := range append([]string{"other.s", "ignored.go"}, names...) {
		if driverutil.CheckReadable(pass, name) != nil {
			test.Fatalf("CheckReadable refuses %s, which the pass holds", name)
		}
		content, readError := readFile(name)
		if readError != nil || string(content) != name {
			test.Errorf("reading %s gave %q and %v, where CheckReadable passes it", name, content, readError)
		}
	}
	_, refusal := readFile("elsewhere.go")
	expected := driverutil.CheckReadable(pass, "elsewhere.go")
	if refusal == nil || expected == nil || refusal.Error() != expected.Error() {
		test.Errorf("reading a name the pass does not hold gave %v, where CheckReadable gives %v", refusal, expected)
	}

	held := pass.Files
	pass.Files = nil
	for _, name := range names {
		if _, readError := readFile(name); readError != nil {
			test.Errorf("reading %s once pass.Files was taken away gave %v: the names were read again", name,
				readError)
		}
	}
	pass.Files = held

	// A pass whose first read is of an other file, then gains a parsed file, reads the gained one, since only a read
	// the other and ignored files miss builds the names.
	otherPass, _ := passOfFiles(test, 2)
	otherRead := driverutil.CheckedReadFile(otherPass, readName)
	if _, readError := otherRead("other.s"); readError != nil {
		test.Fatalf("reading the other file gave %v", readError)
	}
	gained, parseError := parser.ParseFile(otherPass.Fset, "gained.go", "package files\n", parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing gained.go: %v", parseError)
	}
	otherPass.Files = append(otherPass.Files, gained)
	if _, readError := otherRead("gained.go"); readError != nil {
		test.Errorf("reading gained.go after a read of the other file gave %v: that read built the names", readError)
	}
}

// TestCheckedReadFileFromManyGoroutines reads a pass's files from several goroutines at once, as the dispatcher's
// shards do, so the race detector, when the test runs under it, sees the names built and read without a race.
func TestCheckedReadFileFromManyGoroutines(test *testing.T) {
	test.Parallel()
	pass, names := passOfFiles(test, 16)
	readFile := driverutil.CheckedReadFile(pass, readName)
	var group sync.WaitGroup
	failures := make(chan string, 8*len(names))
	for range 8 {
		group.Go(func() {
			for _, name := range names {
				if _, readError := readFile(name); readError != nil {
					failures <- name + ": " + readError.Error()
				}
			}
		})
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		test.Error(failure)
	}
}
