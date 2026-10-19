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
// synthetic one, on every machine, and two consumer themes through their corpora. Two consumers rather
// than one is what shows the list is built from the run's theme rather than from one repository's
// measurement: ahra and www-connected-app share Structure but not their own `@theme`, and their lists
// differ by 150 classes. The synthetic theme holds the shapes none of the others write; see its header.
//
// Regenerate from the repository root with:
//
//	T=internal/lint/rules/tailwind/tools/generate_suggestions
//	D=internal/lint/rules/tailwind/collapse/testdata
//	node $T/class_list.mjs $D/public_theme/theme.css --resolve-root ahra: > $D/class_list_public.txt.gz
//	node $T/class_list.mjs $D/class_list_theme/theme.css --resolve-root ahra: > $D/class_list_synthetic.txt.gz
//	node $T/class_list.mjs ahra:app/_theme/styles/theme.css > $D/class_list.txt.gz
//	node $T/class_list.mjs connected:app/_theme/styles/theme.css > $D/class_list_connected.txt.gz
var classListOracles = []struct {
	name string
	file string
}{
	{name: "public", file: "class_list_public.txt.gz"},
	{name: "synthetic", file: "class_list_synthetic.txt.gz"},
	{name: "ahra", file: "class_list.txt.gz"},
	{name: "connected", file: "class_list_connected.txt.gz"},
}

// TestClassListAgreesWithTailwind holds BuildClassList to Tailwind's own class list, class for class,
// with each class's modifiers compared as a set.
func TestClassListAgreesWithTailwind(t *testing.T) {
	t.Parallel()
	for _, oracle := range classListOracles {
		t.Run(oracle.name, func(t *testing.T) {
			t.Parallel()
			entryPoint, expected := readClassListOracle(t, oracle.file)
			system := oracleSystem(t, entryPoint)

			actual := map[string]string{}
			for _, entry := range BuildClassList(system) {
				actual[entry.Name] = sortedModifiers(entry.Modifiers)
			}

			var missing, extra, modifierDifferences []string
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
			reportClassListDifference(t, "listed by Tailwind and missing here", missing)
			reportClassListDifference(t, "listed here and not by Tailwind", extra)
			reportClassListDifference(t, "listed by both with different modifiers", modifierDifferences)
			t.Logf("%d classes, %d listed alike", len(expected), len(expected)-len(missing)-len(modifierDifferences))
		})
	}
}

// readClassListOracle reads one capture: the entry point its header names and every class with its
// modifiers, space-joined and sorted.
func readClassListOracle(t *testing.T, name string) (string, map[string]string) {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("%s is not gzip: %v", name, err)
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
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if len(classes) == 0 {
		t.Fatalf("%s holds no classes", name)
	}
	return entryPoint, classes
}

func sortedModifiers(modifiers []string) string {
	sorted := append([]string(nil), modifiers...)
	sort.Strings(sorted)
	return strings.Join(sorted, " ")
}

// reportClassListDifference fails with the first few of one kind of difference and how many there are.
func reportClassListDifference(t *testing.T, kind string, names []string) {
	t.Helper()
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	shown := names
	if len(shown) > 20 {
		shown = shown[:20]
	}
	t.Errorf("%d classes %s, starting with:\n  %s", len(names), kind, strings.Join(shown, "\n  "))
}
