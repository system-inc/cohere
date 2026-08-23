package nexus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The gates must not change what masking produces, only how fast it gets there.
func TestShoutingMaskGatesPreserveOutput(t *testing.T) {
	var texts []string
	root := "/Users/kirkouimet/Projects/ahra/libraries/structure/source"
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || len(texts) >= 800 {
			return nil
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		if data, readError := os.ReadFile(path); readError == nil {
			texts = append(texts, string(data))
		}
		return nil
	})
	if len(texts) < 100 {
		t.Skip("corpus unavailable")
	}

	// Ungated reference implementations, copied from before the gates.
	referenceCommand := func(text string) string {
		lines := strings.Split(text, "\n")
		for index, line := range lines {
			if commandLine.MatchString(jsDocGutter.ReplaceAllString(line, "")) {
				lines[index] = strings.Repeat(" ", len(line))
			}
		}
		return strings.Join(lines, "\n")
	}
	referenceExamples := func(text string) string {
		lines := strings.Split(text, "\n")
		insideExample := false
		for index, line := range lines {
			withoutGutter := jsDocGutter.ReplaceAllString(line, "")
			if exampleTag.MatchString(withoutGutter) {
				insideExample = true
				lines[index] = strings.Repeat(" ", len(line))
				continue
			}
			if insideExample && anyJsDocTag.MatchString(withoutGutter) {
				insideExample = false
			}
			if insideExample {
				lines[index] = strings.Repeat(" ", len(line))
			}
		}
		return strings.Join(lines, "\n")
	}

	commandDifferences, exampleDifferences := 0, 0
	for _, text := range texts {
		if maskCommandLines(text) != referenceCommand(text) {
			commandDifferences++
		}
		if maskJsDocExamples(text) != referenceExamples(text) {
			exampleDifferences++
		}
	}
	if commandDifferences != 0 || exampleDifferences != 0 {
		t.Fatalf("gates changed output: %d command diffs, %d example diffs across %d files",
			commandDifferences, exampleDifferences, len(texts))
	}
	t.Logf("gates preserve output across %d files", len(texts))
}

// The gates must actually skip work, or they are pure overhead.
func TestShoutingMaskGatesActuallySkip(t *testing.T) {
	plain := "// just some ordinary prose about the thing\n// with no commands and no tags\n"
	if containsAnyCommandStarter(plain) {
		t.Fatalf("command gate admits plain prose")
	}
	if strings.ContainsRune(plain, '@') {
		t.Fatalf("example gate admits plain prose")
	}

	// And they must admit the real cases, or the rule goes quiet for them.
	for _, admitted := range []string{"$ ls", "git commit", "pnpm install", "curl x", "s c", "ahra os", "sqlite3 db", "npm run"} {
		if !containsAnyCommandStarter(admitted) {
			t.Fatalf("command gate rejects %q, which the pattern matches", admitted)
		}
	}
	// The whitespace arm is the one a literal cannot express, and a fixture is the only thing that
	// catches it: 800 real files contained no multi-space form, so the corpus diff passed while the
	// gate was wrong.
	for _, spacing := range []string{"s c", "s  c", "s\tc"} {
		if !containsAnyCommandStarter(spacing) {
			t.Fatalf("command gate rejects %q, which the pattern matches", spacing)
		}
	}
}
