package tailwind

import (
	"bufio"
	"compress/gzip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// classListOracles are the `getClassList()` captures BuildClassList is held to: the public theme and a
// synthetic one, on every machine. Two themes rather than one is what shows the list is built from the
// run's theme rather than from one measurement: they declare different `@theme` and `@utility` content, so
// a list carried over from either fails the other. The synthetic theme holds the shapes the public one
// doesn't write; see its header.
//
// Regenerate from the repository root with, R being any directory whose node_modules holds tailwindcss 4.3.3:
//
//	T=internal/lint/rules/tailwind/tools/generate_suggestions
//	D=internal/lint/rules/tailwind/collapse/testdata
//	node $T/class_list.mjs $D/public_theme/theme.css --resolve-root $R > $D/class_list_public.txt.gz
//	node $T/class_list.mjs $D/class_list_theme/theme.css --resolve-root $R > $D/class_list_synthetic.txt.gz
var classListOracles = []struct {
	name string
	file string
}{
	{name: "public", file: "class_list_public.txt.gz"},
	{name: "synthetic", file: "class_list_synthetic.txt.gz"},
}

// TestClassListAgreesWithTailwind holds BuildClassList to Tailwind's own class list, class for class,
// with each class's modifiers compared as a set.
func TestClassListAgreesWithTailwind(test *testing.T) {
	test.Parallel()
	for _, oracle := range classListOracles {
		test.Run(oracle.name, func(test *testing.T) {
			test.Parallel()
			entryPoint, expected := readClassListOracle(test, oracle.file)
			system := oracleSystem(test, entryPoint)

			actual := map[string]string{}
			for _, entry := range BuildClassList(system) {
				actual[entry.Name] = sortedModifiers(entry.Modifiers)
			}

			var missing []string
			var extra []string
			var modifierDifferences []string
			for name, modifiers := range expected {
				got, listed := actual[name]
				if !listed {
					missing = append(missing, name)
					continue
				}
				if got != modifiers {
					modifierDifferences = append(modifierDifferences, name+": Tailwind ["+modifiers+"], ours ["+got+"]")
				}
			}
			for name := range actual {
				if _, listed := expected[name]; !listed {
					extra = append(extra, name)
				}
			}
			reportClassListDifference(test, "listed by Tailwind and missing here", missing)
			reportClassListDifference(test, "listed here and not by Tailwind", extra)
			reportClassListDifference(test, "listed by both with different modifiers", modifierDifferences)
			test.Logf("%d classes, %d listed alike", len(expected), len(expected)-len(missing)-len(modifierDifferences))
		})
	}
}

// readClassListOracle reads one capture: the entry point its header names and every class with its
// modifiers, space-joined and sorted.
func readClassListOracle(test *testing.T, name string) (string, map[string]string) {
	test.Helper()
	file, openError := os.Open(filepath.Join("testdata", name))
	if openError != nil {
		test.Fatalf("open %s: %v", name, openError)
	}
	// Ignored: the capture is only read, so a failed close loses nothing.
	defer file.Close()
	reader, gzipError := gzip.NewReader(file)
	if gzipError != nil {
		test.Fatalf("%s is not gzip: %v", name, gzipError)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	entryPoint := ""
	classes := map[string]string{}
	for scanner.Scan() {
		line := scanner.Text()
		if header, isHeader := strings.CutPrefix(line, "# "); isHeader {
			if _, after, found := strings.Cut(header, " entry "); found {
				entryPoint = after
			}
			continue
		}
		className, modifiers, _ := strings.Cut(line, "\t")
		classes[className] = modifiers
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatalf("read %s: %v", name, scanError)
	}
	if len(classes) == 0 {
		test.Fatalf("%s holds no classes", name)
	}
	return entryPoint, classes
}

func sortedModifiers(modifiers []string) string {
	sorted := append([]string(nil), modifiers...)
	sort.Strings(sorted)
	return strings.Join(sorted, " ")
}

// reportClassListDifference fails with the first few of one kind of difference and how many there are.
func reportClassListDifference(test *testing.T, kind string, names []string) {
	test.Helper()
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	shown := names
	if len(shown) > 20 {
		shown = shown[:20]
	}
	test.Errorf("%d classes %s, starting with:\n  %s", len(names), kind, strings.Join(shown, "\n  "))
}
