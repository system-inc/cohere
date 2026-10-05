package testpolicy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func names(t *testing.T, source string) []string {
	t.Helper()
	violations, err := SerialTests("planted_test.go", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, violation := range violations {
		found = append(found, violation.Name)
	}
	return found
}

// TestSerialTestsFindsAPlantedSerialTest is the checker against planted tests, each beside its control.
func TestSerialTestsFindsAPlantedSerialTest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"a serial test", "func TestA(t *testing.T) { work() }", "TestA"},
		{"its parallel control", "func TestA(t *testing.T) { t.Parallel(); work() }", ""},
		{"excused with a reason", "// Not parallel: it sets GOFLAGS.\nfunc TestA(t *testing.T) { work() }", ""},
		{"excused under other prose", "// TestA checks a thing.\n//\n// Not parallel: it sets GOFLAGS.\nfunc TestA(t *testing.T) { work() }", ""},
		{"a marker with no reason", "// Not parallel:\nfunc TestA(t *testing.T) { work() }", "TestA"},
		{"Parallel in a closure marks nothing", "func TestA(t *testing.T) { func() { t.Parallel() }() }", "TestA"},
		{"another receiver's Parallel", "func TestA(t *testing.T) { other.Parallel() }", "TestA"},
		{"a serial subtest", "func TestA(t *testing.T) { t.Parallel(); t.Run(\"sub\", func(t *testing.T) { work() }) }", "TestA/sub"},
		{"its parallel control", "func TestA(t *testing.T) { t.Parallel(); t.Run(\"sub\", func(t *testing.T) { t.Parallel() }) }", ""},
		{"an excused subtest", "func TestA(t *testing.T) {\n\tt.Parallel()\n\t// Not parallel: the subtests share one fixture in order.\n\tt.Run(\"sub\", func(t *testing.T) { work() })\n}", ""},
		{"not a test", "func helper(t *testing.T) { work() }\nfunc Testable(t *testing.T) { work() }\nfunc TestMain(m *testing.M) { m.Run() }", ""},
		{"a benchmark is not a test", "func BenchmarkA(b *testing.B) { work() }", ""},
	}
	for _, testCase := range cases {
		source := "package p\n\nimport \"testing\"\n\n" + testCase.source + "\n"
		found := strings.Join(names(t, source), ",")
		if found != testCase.want {
			t.Errorf("%s: found %q, expected %q", testCase.name, found, testCase.want)
		}
	}
}

// TestEveryCohereTestRunsInParallel holds the module to the house default: every test and subtest calls
// t.Parallel(), or the comment above it says why it cannot (#nxgt2ca).
func TestEveryCohereTestRunsInParallel(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var violations []Violation
	files := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "TypeScript", "testdata", "node_modules", ".cache", ".git", ".build":
				return filepath.SkipDir
			}
			// A directory with a go.mod of its own is another module (the TypeScript shims), which this
			// module's go test ./... never runs, so its tests are not this rule's to hold.
			if path != root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := SerialTests(path, source)
		if err != nil {
			return err
		}
		files++
		violations = append(violations, found...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The control: a walk that read nothing would pass.
	if files < 100 {
		t.Fatalf("read only %d test files under %s", files, root)
	}
	if len(violations) == 0 {
		return
	}
	var lines []string
	for _, violation := range violations {
		relative, _ := filepath.Rel(root, violation.Position.Filename)
		lines = append(lines, fmt.Sprintf("  %s:%d %s", relative, violation.Position.Line, violation.Name))
	}
	t.Errorf("%d tests of %d files neither call t.Parallel() nor say why above them with %q and a reason:\n%s",
		len(violations), files, NotParallelMarker, strings.Join(lines, "\n"))
}
