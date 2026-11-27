package tailwind

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// The collapses #sbb2hke found the rule missing or misspelling, each answered as Tailwind 4.3.3's
// canonicalizeCandidates answers it with the rule's options, `{collapse: true, logicalToPhysical: true}`,
// on the theme the case names. Measured over a grid of pairs (w+h, ml+mr, mt+mb, pt+pb, pl+pr, px+py,
// gap-x+gap-y, inset-x+inset-y, top+bottom, scroll-mt+scroll-mb, translate-x+translate-y,
// border-spacing-x+border-spacing-y, -ml+-mr) by values (px, 0, 4, 2.5, 1/2, full, auto, screen, rail,
// gutter, [3px], [3.5rem], [1rem]): before, the rule differed from Tailwind on 75 of the planted theme's
// 131 known pairs and 49 of the public theme's 105; after, on none. Every case below was asked of Tailwind
// on the theme it runs on.

// canonicalTheme is a committed theme split across files, read into the paths a fixture writes them to:
// theme.css where the rule's entry-point search finds it, and its imports beside it.
func canonicalTheme(test *testing.T, directory string) map[string]string {
	test.Helper()
	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil {
		test.Fatalf("reading the theme at %s: %v", directory, readDirectoryError)
	}
	stylesheets := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".css" {
			continue
		}
		contents, readError := os.ReadFile(filepath.Join(directory, entry.Name()))
		if readError != nil {
			test.Fatalf("reading %s: %v", entry.Name(), readError)
		}
		stylesheets[path.Join(path.Dir(unknownFixtureStylesheetPath), entry.Name())] = string(contents)
	}
	if _, hasEntryPoint := stylesheets[unknownFixtureStylesheetPath]; !hasEntryPoint {
		test.Fatalf("the theme at %s has no theme.css", directory)
	}
	return stylesheets
}

// plantedCanonicalTheme is the planted repository's theme, whose `--spacing-rail` (3.5rem) and
// `--spacing-gutter` (1.25rem) are spacing keys that equal a multiple of its 0.25rem `--spacing`.
func plantedCanonicalTheme(test *testing.T) map[string]string {
	test.Helper()
	return canonicalTheme(test, filepath.Join("collapse", "testdata", "planted_repository", "styles"))
}

// publicCanonicalTheme is the public theme, which declares no spacing key of its own.
func publicCanonicalTheme(test *testing.T) map[string]string {
	test.Helper()
	return canonicalTheme(test, filepath.Join("collapse", "testdata", "public_theme"))
}

// canonicalCollapseCase is one class list and the collapses Tailwind reports for it, in the order the
// rule reports them; the last output is the class the list ends as.
type canonicalCollapseCase struct {
	source  string
	outputs []string
	// inputs, when set, are the classes the first finding must name as the author wrote them.
	inputs []string
}

// expectCanonicalCollapses runs each case on a theme and holds the rule to its collapses: one finding per
// output, each a collapse naming its output, and nothing else.
func expectCanonicalCollapses(test *testing.T, stylesheets map[string]string, cases []canonicalCollapseCase) {
	test.Helper()
	for _, testCase := range cases {
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			source := `const element = <div className="` + testCase.source + `" />;`
			result := runCanonicalFixtureWithStylesheets(test, "Component.tsx", source, stylesheets, nil)
			wantIds := make([]string, len(testCase.outputs))
			for index := range wantIds {
				wantIds[index] = "canonicalCollapse"
			}
			rule_testing.ExpectFindings(test, result, wantIds...)
			for index, output := range testCase.outputs {
				description := result.Diagnostics[index].Message.Description
				if !strings.Contains(description, `as "`+output+`".`) {
					test.Errorf("finding %d should name %q: %s", index, output, description)
				}
			}
			if testCase.inputs != nil {
				named := `"` + strings.Join(testCase.inputs, `", "`) + `" say the same thing`
				if description := result.Diagnostics[0].Message.Description; !strings.Contains(description, named) {
					test.Errorf("the finding should name the classes as written, %v: %s", testCase.inputs, description)
				}
			}
		})
	}
}

// expectNoCanonicalFinding runs each class list on a theme and holds the rule silent on it.
func expectNoCanonicalFinding(test *testing.T, stylesheets map[string]string, sources []string) {
	test.Helper()
	for _, classes := range sources {
		test.Run(classes, func(test *testing.T) {
			test.Parallel()
			source := `const element = <div className="` + classes + `" />;`
			result := runCanonicalFixtureWithStylesheets(test, "Component.tsx", source, stylesheets, nil)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// staticSpacingCollapses are pairs whose classes are statics, `w-px` and `ml-auto` among them, that
// Tailwind collapses all the same: its collapse reads a static by its first functional reading with a
// value and keeps the shorthand when it signs as the pair does. The rule read only a class's first
// reading, found a static, and stayed silent.
var staticSpacingCollapses = []canonicalCollapseCase{
	{source: "w-px h-px", outputs: []string{"size-px"}},
	{source: "ml-px mr-px", outputs: []string{"mx-px"}},
	{source: "pt-px pb-px", outputs: []string{"py-px"}},
	{source: "px-px py-px", outputs: []string{"p-px"}},
	{source: "gap-x-px gap-y-px", outputs: []string{"gap-px"}},
	{source: "inset-x-px inset-y-px", outputs: []string{"inset-px"}},
	{source: "-ml-px -mr-px", outputs: []string{"-mx-px"}},
	{source: "w-full h-full", outputs: []string{"size-full"}},
	{source: "w-auto h-auto", outputs: []string{"size-auto"}},
	{source: "ml-auto mr-auto", outputs: []string{"mx-auto"}},
	{source: "top-auto bottom-auto", outputs: []string{"inset-y-auto"}},
	{source: "translate-x-full translate-y-full", outputs: []string{"translate-full"}},
	{source: "sm:w-px sm:h-px", outputs: []string{"sm:size-px"}},
}

// staticSpacingSilences are the near misses: `w-screen` and `h-screen` share the value name and not the
// value, 100vw against 100vh, so there is no `size-screen` to merge into, and the rest differ in value or
// variant.
var staticSpacingSilences = []string{
	"w-screen h-screen",
	"w-px h-full",
	"w-px sm:h-px",
	"ml-auto mr-px",
}

func TestCanonicalCollapseReadsStaticSpacingOnThePlantedTheme(test *testing.T) {
	test.Parallel()
	stylesheets := plantedCanonicalTheme(test)
	expectCanonicalCollapses(test, stylesheets, staticSpacingCollapses)
	expectNoCanonicalFinding(test, stylesheets, staticSpacingSilences)
}

func TestCanonicalCollapseReadsStaticSpacingOnThePublicTheme(test *testing.T) {
	test.Parallel()
	stylesheets := publicCanonicalTheme(test)
	expectCanonicalCollapses(test, stylesheets, staticSpacingCollapses)
	expectNoCanonicalFinding(test, stylesheets, staticSpacingSilences)
}

// TestCanonicalCollapseIsSpelledAsTailwindSpellsIt is the collapse's spelling. Upstream keeps the first
// listed class that signs as the pair does, in getClassList order, so a named spacing key equal to a
// multiple collapses to the multiple: `w-rail h-rail` is `size-14`, not the `size-rail` the family's root
// swap prints. `w-rail` alone stays as written, because two listed classes, `w-14` and `w-rail`, sign
// alike and the single-class pass bails on two; so does `w-[3.5rem]`, and collapsed it is `size-14` too.
func TestCanonicalCollapseIsSpelledAsTailwindSpellsIt(test *testing.T) {
	test.Parallel()
	stylesheets := plantedCanonicalTheme(test)
	expectCanonicalCollapses(test, stylesheets, []canonicalCollapseCase{
		{source: "w-rail h-rail", outputs: []string{"size-14"}},
		{source: "px-rail py-rail", outputs: []string{"p-14"}},
		{source: "w-gutter h-gutter", outputs: []string{"size-5"}},
		{source: "ml-rail mr-rail", outputs: []string{"mx-14"}},
		{source: "gap-x-gutter gap-y-gutter", outputs: []string{"gap-5"}},
		{source: "top-rail bottom-rail", outputs: []string{"inset-y-14"}},
		{source: "-ml-rail -mr-rail", outputs: []string{"-mx-14"}},
		{source: "w-[3.5rem] h-[3.5rem]", outputs: []string{"size-14"}},
		{source: "sm:w-rail sm:h-rail", outputs: []string{"sm:size-14"}},
		{source: "w-rail! h-rail!", outputs: []string{"size-14!"}},
		// The chain spells every step: `my-14`, then `mx-14`, then `m-14`.
		{source: "mt-rail mb-rail ml-rail mr-rail", outputs: []string{"my-14", "mx-14", "m-14"}},
		// A value no listed class signs as keeps the root swap's spelling, as upstream's does.
		{source: "w-[3px] h-[3px]", outputs: []string{"size-[3px]"}},
	})
	expectNoCanonicalFinding(test, stylesheets, []string{"w-rail", "w-rail h-gutter"})

	// The public theme has no spacing key, so the arbitrary value is the case that reaches the listed class.
	expectCanonicalCollapses(test, publicCanonicalTheme(test), []canonicalCollapseCase{
		{source: "w-[3.5rem] h-[3.5rem]", outputs: []string{"size-14"}},
	})
}

// TestCanonicalCollapseOfClassesThatRewriteAlone is pairs whose classes Tailwind also rewrites on their
// own. Upstream rewrites each before it collapses any, so `w-[1rem] h-[1rem]` collapses as `w-4 h-4`, to
// `size-4` and not `size-[1rem]`, and `-ml-0 -mr-0` as `ml-0 mr-0`, to `mx-0`. The rule reaches the same
// class from the pair as written: `size-[1rem]` signs as the listed `size-4`, and `-mx-0` is dropped from
// the list where it signs as `mx-0`, as upstream drops it. The finding names the classes the author wrote,
// and no single is reported beside it.
func TestCanonicalCollapseOfClassesThatRewriteAlone(test *testing.T) {
	test.Parallel()
	cases := []canonicalCollapseCase{
		{source: "w-[1rem] h-[1rem]", outputs: []string{"size-4"}, inputs: []string{"w-[1rem]", "h-[1rem]"}},
		{source: "-ml-0 -mr-0", outputs: []string{"mx-0"}, inputs: []string{"-ml-0", "-mr-0"}},
		{source: "px-[1rem] py-4", outputs: []string{"p-4"}, inputs: []string{"px-[1rem]", "py-4"}},
	}
	expectCanonicalCollapses(test, plantedCanonicalTheme(test), cases)
	expectCanonicalCollapses(test, publicCanonicalTheme(test), cases)
}

// TestCanonicalCollapseComparesValuesBySignature is how upstream decides two classes share a value: by
// what they compile to, not by how the value is spelled. `w-rail h-14` set width and height to the same
// 3.5rem on the planted theme, so they collapse to `size-14`, while `px-4 py-2` sign as no `p-*` class and
// stay, as do `w-rail h-gutter`, 3.5rem against 1.25rem.
func TestCanonicalCollapseComparesValuesBySignature(test *testing.T) {
	test.Parallel()
	stylesheets := plantedCanonicalTheme(test)
	expectCanonicalCollapses(test, stylesheets, []canonicalCollapseCase{
		{source: "w-rail h-14", outputs: []string{"size-14"}},
		{source: "w-gutter h-5", outputs: []string{"size-5"}},
		{source: "pl-rail pr-14", outputs: []string{"px-14"}},
		{source: "w-rail h-[3.5rem]", outputs: []string{"size-14"}, inputs: []string{"w-rail", "h-[3.5rem]"}},
		{source: "mt-px mb-[1px]", outputs: []string{"my-px"}},
	})
	expectNoCanonicalFinding(test, stylesheets, []string{"w-rail h-gutter", "px-4 py-2", "w-rail sm:h-14"})

	expectCanonicalCollapses(test, publicCanonicalTheme(test), []canonicalCollapseCase{
		{source: "w-px h-[1px]", outputs: []string{"size-px"}},
		{source: "w-full h-[100%]", outputs: []string{"size-full"}},
		{source: "px-4 py-[1rem]", outputs: []string{"p-4"}},
	})
}
