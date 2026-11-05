package program

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// buildFixtureGraph writes a small program to disk and builds a real type graph over it.
//
// A real graph rather than a hand-built map, because the thing under test is whether the reversal
// agrees with how the compiler actually keys its resolutions. A fixture that constructs the map
// itself would have passed while the real reversal produced almost no edges, which is the defect
// this file exists to have caught.
func buildFixtureGraph(test *testing.T, files map[string]string) *Graph {
	test.Helper()
	directory := test.TempDir()

	for name, contents := range files {
		path := filepath.Join(directory, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}

	configuration := `{"compilerOptions":{"strict":true,"module":"esnext","target":"esnext","moduleResolution":"bundler"},"include":["**/*.ts"]}`
	if writeError := os.WriteFile(
		filepath.Join(directory, "tsconfig.json"),
		[]byte(configuration),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}

	graph, buildError := Build(Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building the fixture graph: %v", buildError)
	}
	return graph
}

func fileNamed(test *testing.T, graph *Graph, suffix string) *ast.SourceFile {
	test.Helper()
	for _, sourceFile := range graph.ProjectFiles() {
		if strings.HasSuffix(sourceFile.FileName().AsString(), suffix) {
			return sourceFile
		}
	}
	test.Fatalf("no project file ends in %s", suffix)
	return nil
}

func closureNames(closure []*ast.SourceFile) []string {
	names := make([]string, 0, len(closure))
	for _, sourceFile := range closure {
		names = append(names, filepath.Base(sourceFile.FileName().AsString()))
	}
	return names
}

// TestDependentClosureReachesConsumersThroughImports is the case that motivated the whole scope.
//
// A change to Producer breaks Consumer, which the edit never touched, so a run scoped to Producer
// alone reports nothing about it. Measured on the real tree before this existed: one edited file
// produced seven new findings, four of them in a consumer two directories away.
func TestDependentClosureReachesConsumersThroughImports(test *testing.T) {
	test.Parallel()
	graph := buildFixtureGraph(test, map[string]string{
		"Producer.ts":  "export function value(): number {\n  return 1;\n}\n",
		"Consumer.ts":  "import { value } from './Producer';\nexport const doubled = value() * 2;\n",
		"Indirect.ts":  "import { doubled } from './Consumer';\nexport const tripled = doubled * 3;\n",
		"Unrelated.ts": "export const alone = 1;\n",
	})

	closure, within := graph.DependentClosure([]*ast.SourceFile{fileNamed(test, graph, "Producer.ts")})
	if !within {
		test.Fatal("a four-file program exceeded the closure limit")
	}

	names := strings.Join(closureNames(closure), " ")
	for _, want := range []string{"Producer.ts", "Consumer.ts", "Indirect.ts"} {
		if !strings.Contains(names, want) {
			test.Errorf("%s should be in the closure, got %s", want, names)
		}
	}

	// The control. Without it a closure that returned every project file would satisfy every
	// assertion above, which is the shape the defect actually took: a broken reversal that silently
	// returned too little read the same as one returning too much.
	if strings.Contains(names, "Unrelated.ts") {
		test.Errorf("a file that imports nothing in the chain was in the closure: %s", names)
	}
}

// TestDependentClosureDoesNotFollowImportsForward holds the direction.
//
// Consumer imports Producer, so editing Consumer cannot break Producer. A closure that walked the
// resolution map without reversing it would return the wrong set and still look plausible: same
// size, same shape, findings in files that were genuinely related to the edit.
func TestDependentClosureDoesNotFollowImportsForward(test *testing.T) {
	test.Parallel()
	graph := buildFixtureGraph(test, map[string]string{
		"Producer.ts": "export function value(): number {\n  return 1;\n}\n",
		"Consumer.ts": "import { value } from './Producer';\nexport const doubled = value() * 2;\n",
	})

	closure, within := graph.DependentClosure([]*ast.SourceFile{fileNamed(test, graph, "Consumer.ts")})
	if !within {
		test.Fatal("a two-file program exceeded the closure limit")
	}

	names := strings.Join(closureNames(closure), " ")
	if strings.Contains(names, "Producer.ts") {
		test.Errorf("the closure followed an import forwards: %s", names)
	}
	if !strings.Contains(names, "Consumer.ts") {
		test.Errorf("the seed itself was not in its own closure: %s", names)
	}
}

// TestDependentClosureRefusesRatherThanTruncating holds the failure direction.
//
// Above the limit the answer is `check everything`, never a partial set. A truncated closure would
// be fast, silent, and wrong in the direction that hides findings, which is the one failure mode
// scoping must not introduce.
func TestDependentClosureRefusesRatherThanTruncating(test *testing.T) {
	test.Parallel()
	files := map[string]string{"Producer.ts": "export function value(): number {\n  return 1;\n}\n"}
	for index := 0; index <= DependentClosureLimit; index++ {
		files[filepath.Join("consumers", "Consumer"+itoa(index)+".ts")] =
			"import { value } from '../Producer';\nexport const used" + itoa(index) + " = value();\n"
	}

	graph := buildFixtureGraph(test, files)

	if _, within := graph.DependentClosure([]*ast.SourceFile{fileNamed(test, graph, "Producer.ts")}); within {
		test.Fatal("a closure past the limit reported itself as usable")
	}

	// The control: the same graph, seeded at a file nothing imports, is comfortably inside the limit.
	// Without it a function that always refused would pass the assertion above.
	if _, within := graph.DependentClosure([]*ast.SourceFile{fileNamed(test, graph, "Consumer0.ts")}); !within {
		test.Error("a leaf file's closure was reported as too large")
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
