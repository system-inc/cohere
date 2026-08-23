package prettier

import (
	"os"
	"strings"
	"testing"
)

// newTestEngine builds an engine, skipping when the fork's bundles are not on this machine.
//
// Skipping rather than failing because the bundle path is still a local checkout until the build
// step vendors them. A skip says "not measured here"; a pass would say "measured and fine", and the
// difference is the whole reason this package exists.
func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	if _, err := os.Stat(bundleDirectory); err != nil {
		t.Skipf("prettier bundles not present at %s", bundleDirectory)
	}
	engine, err := New(DefaultOptions())
	if err != nil {
		t.Fatalf("building the engine: %v", err)
	}
	return engine
}

// TestFormatsCrampedSource is the known-dirty control. A formatter that has never changed anything
// has not been shown to run at all.
func TestFormatsCrampedSource(t *testing.T) {
	engine := newTestEngine(t)
	formatted, err := engine.Format("probe.ts", "const   x=1\n")
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if formatted != "const x = 1;\n" {
		t.Fatalf("formatted %q, want %q", formatted, "const x = 1;\n")
	}
}

// TestLeavesFormattedSourceAlone is the other half of the control. A formatter that rewrites
// everything is as useless as one that rewrites nothing.
func TestLeavesFormattedSourceAlone(t *testing.T) {
	engine := newTestEngine(t)
	source := "const x = 1;\n"
	formatted, err := engine.Format("clean.ts", source)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if formatted != source {
		t.Fatalf("formatted %q, want it unchanged", formatted)
	}
}

// TestPromiseIsDrained is the regression for the least obvious failure in this package.
//
// prettier.format returns a promise and goja has no event loop, so a version that stringifies the
// result without pumping the job queue writes the literal text "[object Promise]" into the file.
// That is what the first working version produced.
func TestPromiseIsDrained(t *testing.T) {
	engine := newTestEngine(t)
	formatted, err := engine.Format("probe.ts", "const x=1\n")
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if strings.Contains(formatted, "Promise") {
		t.Fatalf("the promise was not drained: %q", formatted)
	}
}

// TestForkCustomizationsSurvive pins the four divergences our fork carries on top of upstream. They
// are the exact thing a reimplementation loses silently, and the reason we run the fork rather than
// stock Prettier.
func TestForkCustomizationsSurvive(t *testing.T) {
	engine := newTestEngine(t)
	source := "function probe(a) {\n" +
		"    if(a) {\n        a = 1;\n    }\n" +
		"    else {\n        a = 2;\n    }\n" +
		"    for(const x of [a]) {\n        a = x;\n    }\n" +
		"    while(a) {\n        a = 0;\n    }\n" +
		"    switch(a) {\n        default:\n            break;\n    }\n" +
		"    const f = function() {\n        return a;\n    };\n" +
		"    return f();\n}\n"
	formatted, err := engine.Format("probe.js", source)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	for _, want := range []string{"if(a)", "for(const", "while(a)", "switch(a)", "function()", "\n    else {"} {
		if !strings.Contains(formatted, want) {
			t.Errorf("fork customization lost: %q missing from\n%s", want, formatted)
		}
	}
}

// TestMarkdownTildeIsNotDoubled is the regression for the goja \p{...} bug.
//
// goja implements no Unicode property escapes and returns false for every character rather than
// throwing, which inverted CommonMark's flanking calculation and turned "approximately 25K" into
// strikethrough. The fix lives in the fork's build; this proves the bundles being loaded carry it.
func TestMarkdownTildeIsNotDoubled(t *testing.T) {
	engine := newTestEngine(t)
	source := "Sizes: `Seed` (~25K tokens), `Warm` (~75K tokens).\n"
	formatted, err := engine.Format("probe.md", source)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if strings.Contains(formatted, "~~") {
		t.Fatalf("a standalone tilde was doubled, so the bundles predate the fix: %q", formatted)
	}
}

// TestUnhandledTypeIsAnError proves the engine refuses rather than silently passing a file through.
func TestUnhandledTypeIsAnError(t *testing.T) {
	engine := newTestEngine(t)
	if _, err := engine.Format("probe.rb", "puts 1\n"); err == nil {
		t.Fatal("formatting a .rb file succeeded, want an error")
	}
	if engine.Handles("probe.rb") {
		t.Fatal("Handles said yes to .rb")
	}
}

// TestSyntaxErrorSurfaces confirms a malformed file produces an error whose text the pipeline's
// matcher recognizes as a parse failure rather than a formatter crash.
func TestSyntaxErrorSurfaces(t *testing.T) {
	engine := newTestEngine(t)
	_, err := engine.Format("broken.ts", "const x = {{{ broken\n")
	if err == nil {
		t.Fatal("formatting malformed source succeeded, want an error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "syntaxerror") {
		t.Fatalf("error %q does not name a syntax error, so the pipeline will count it as a crash", err)
	}
}
