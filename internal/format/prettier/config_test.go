package prettier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveFindsTheNearestConfigAbove pins inheritance: a nested library with no Prettier key of its
// own formats with its containing repository's options, as libraries/structure does inside ahra.
func TestResolveFindsTheNearestConfigAbove(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"prettier": {"tabWidth": 4, "printWidth": 120, "singleQuote": true, "bracketSameLine": true}}`)
	writeFile(t, filepath.Join(root, "libraries", "structure", "package.json"), `{"name": "structure"}`)

	resolution, err := ResolveOptions(filepath.Join(root, "libraries", "structure", "source"))
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Source != filepath.Join(root, "package.json") {
		t.Fatalf("resolved from %q, want the root package.json", resolution.Source)
	}
	if resolution.Options.TabWidth != 4 || resolution.Options.PrintWidth != 120 || !resolution.Options.SingleQuote || !resolution.Options.BracketSameLine {
		t.Fatalf("options %+v did not take the config", resolution.Options)
	}
}

// TestResolveWithoutConfigIsPrettierNotAhra holds the distinction a fallback would erase.
func TestResolveWithoutConfigIsPrettierNotAhra(t *testing.T) {
	resolution, err := ResolveOptions(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Source != "" {
		t.Skipf("a config exists above the temp directory (%s), so this machine cannot test the empty case", resolution.Source)
	}
	if resolution.Options != PrettierDefaults() || resolution.Options.TabWidth != 2 || resolution.Options.PrintWidth != 80 {
		t.Fatalf("no config resolved to %+v, want Prettier's own defaults", resolution.Options)
	}
}

// TestResolveRefusesWhatItCannotApply covers each refusal, because each one replaces a silent default.
func TestResolveRefusesWhatItCannotApply(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"unknown option":    {"package.json": `{"prettier": {"quoteProps": "consistent"}}`},
		"shared config":     {"package.json": `{"prettier": "@company/prettier-config"}`},
		"javascript config": {"prettier.config.js": `module.exports = {}`},
		"crlf":              {".prettierrc": `{"endOfLine": "crlf"}`},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for file, contents := range files {
				writeFile(t, filepath.Join(root, file), contents)
			}
			if resolution, err := ResolveOptions(root); err == nil {
				t.Fatalf("resolved %+v from %s, want a refusal", resolution.Options, resolution.Source)
			}
		})
	}
}

// TestResolveNamesPluginKeysItDoesNotApply keeps the Tailwind plugin's settings visible.
func TestResolveNamesPluginKeysItDoesNotApply(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{"prettier": {"plugins": ["prettier-plugin-tailwindcss"], "tailwindFunctions": ["mergeClassNames"], "tabWidth": 4}}`)
	resolution, err := ResolveOptions(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(resolution.NotApplied, ",") != "plugins,tailwindFunctions" {
		t.Fatalf("not applied %v, want plugins and tailwindFunctions named", resolution.NotApplied)
	}
}

// TestBracketSameLineReachesTheEngine proves the option is passed, not merely carried.
//
// A field resolved and never handed to Prettier would pass every test above. This formats the same
// JSX both ways and requires different bytes, the specific difference being where `>` goes.
func TestBracketSameLineReachesTheEngine(t *testing.T) {
	source := "const element = <Component firstAttribute=\"a long value here\" secondAttribute=\"another long value\" third=\"x\">child</Component>;\n"

	format := func(sameLine bool) string {
		options := DefaultOptions()
		options.PrintWidth = 60
		options.BracketSameLine = sameLine
		engine, err := New(options)
		if err != nil {
			t.Fatal(err)
		}
		formatted, err := engine.Format("probe.tsx", source)
		if err != nil {
			t.Fatal(err)
		}
		return formatted
	}

	apart, together := format(false), format(true)
	if apart == together {
		t.Fatalf("bracketSameLine changed nothing:\n%s", apart)
	}
	if !strings.Contains(together, "\"x\">") || strings.Contains(apart, "\"x\">") {
		t.Fatalf("expected `>` on the attribute line only with bracketSameLine:\nfalse:\n%s\ntrue:\n%s", apart, together)
	}
}
