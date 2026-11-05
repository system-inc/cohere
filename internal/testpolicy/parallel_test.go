package testpolicy

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func names(test *testing.T, source string) []string {
	test.Helper()
	violations, serialTestsError := SerialTests("planted_test.go", []byte(source))
	if serialTestsError != nil {
		test.Fatal(serialTestsError)
	}
	var found []string
	for _, violation := range violations {
		found = append(found, violation.Name)
	}
	return found
}

// TestSerialTestsFindsAPlantedSerialTest is the checker against planted tests, each beside its control.
func TestSerialTestsFindsAPlantedSerialTest(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"a serial test", "func TestA(t *testing.T) { work() }", "TestA"},
		{"its parallel control", "func TestA(t *testing.T) { t.Parallel(); work() }", ""},
		{"excused with a reason", "// Not parallel: it sets GOFLAGS.\nfunc TestA(t *testing.T) { work() }", ""},
		{
			"excused under other prose",
			"// TestA checks a thing.\n//\n// Not parallel: it sets GOFLAGS.\nfunc TestA(t *testing.T) { work() }",
			"",
		},
		{"a marker with no reason", "// Not parallel:\nfunc TestA(t *testing.T) { work() }", "TestA"},
		{"Parallel in a closure marks nothing", "func TestA(t *testing.T) { func() { t.Parallel() }() }", "TestA"},
		{"another receiver's Parallel", "func TestA(t *testing.T) { other.Parallel() }", "TestA"},
		{
			"a serial subtest",
			"func TestA(t *testing.T) { t.Parallel(); t.Run(\"sub\", func(t *testing.T) { work() }) }",
			"TestA/sub",
		},
		{
			"its parallel control",
			"func TestA(t *testing.T) { t.Parallel(); t.Run(\"sub\", func(t *testing.T) { t.Parallel() }) }",
			"",
		},
		{
			"an excused subtest",
			"func TestA(t *testing.T) {\n\tt.Parallel()\n\t// Not parallel: the subtests share one fixture in order.\n\tt.Run(\"sub\", func(t *testing.T) { work() })\n}",
			"",
		},
		{
			"not a test",
			"func helper(t *testing.T) { work() }\nfunc Testable(t *testing.T) { work() }\nfunc TestMain(m *testing.M) { m.Run() }",
			"",
		},
		{"a benchmark is not a test", "func BenchmarkA(b *testing.B) { work() }", ""},
	}
	for _, testCase := range cases {
		source := "package p\n\nimport \"testing\"\n\n" + testCase.source + "\n"
		found := strings.Join(names(test, source), ",")
		if found != testCase.want {
			test.Errorf("%s: found %q, expected %q", testCase.name, found, testCase.want)
		}
	}
}

// TestEveryCohereTestRunsInParallel holds the module to the house default: every test and subtest calls
// test.Parallel(), or the comment above it says why it cannot (#nxgt2ca).
func TestEveryCohereTestRunsInParallel(test *testing.T) {
	test.Parallel()
	var violations []Violation
	root, files := eachTestFile(test, func(path string, source []byte) error {
		found, serialTestsError := SerialTests(path, source)
		violations = append(violations, found...)
		return serialTestsError
	})
	if len(violations) == 0 {
		return
	}
	var lines []string
	for _, violation := range violations {
		relative, _ := filepath.Rel(root, violation.Position.Filename)
		lines = append(lines, fmt.Sprintf("  %s:%d %s", relative, violation.Position.Line, violation.Name))
	}
	test.Errorf("%d tests of %d files neither call t.Parallel() nor say why above them with %q and a reason:\n%s",
		len(violations), files, NotParallelMarker, strings.Join(lines, "\n"))
}
