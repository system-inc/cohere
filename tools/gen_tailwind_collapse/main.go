// Command gen_tailwind_collapse regenerates the Tailwind collapse table from the installed Tailwind.
//
// Usage:
//
//	go run ./tools/gen_tailwind_collapse -entry-point <theme.css> [-output <file.go>] [-check]
//
// The table it writes is what lets `enforce-canonical-classes` run in Go without a JavaScript engine
// at lint time. Tailwind's own collapse logic is a signature-equivalence search over the whole
// utility registry, eight to ten thousand lines that change every minor release. Porting the search
// was rejected twice for that reason. Porting its answers is a different proposition: 327 functional
// roots produce 53,301 pairs, of which 38 collapse, and the families are facts about roots rather
// than computations per class.
//
// The generator exists so that staleness is visible instead of silent. Today a Tailwind upgrade can
// change what collapses and nobody finds out, because the only way to notice is a finding that stops
// being reported. With a generated table, an upgrade produces a diff in a checked-in file, which a
// human reads in review. `-check` makes that a gate: it regenerates and fails if the committed table
// disagrees, so a version bump that changes the answers cannot land quietly.
//
// Node is required to run it and never to use the result. The enumeration asks the real Tailwind
// engine, because a hand-maintained table is a guess and the guesses have been wrong: an earlier
// attempt enumerated from the classes this codebase happens to use and missed nine families.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// enumeration is what enumerate.mjs reports.
type enumeration struct {
	TailwindVersion string   `json:"tailwindVersion"`
	EntryPoint      string   `json:"entryPoint"`
	FunctionalRoots int      `json:"functionalRoots"`
	StaticUtilities int      `json:"staticUtilities"`
	PairsProbed     int      `json:"pairsProbed"`
	Families        []family `json:"families"`
	// RootProperties and StaticProperties are what `no-conflicting-classes` needs: the CSS property
	// names each utility declares. Measured as a fact about roots rather than about classes, so
	// `px-4` and `px-8` share an entry and the table does not grow with the theme's scale.
	RootProperties      []utilityProperties `json:"rootProperties"`
	RootColorProperties []utilityProperties `json:"rootColorProperties"`
	RootSelectorShapes  []rootSelectorShape `json:"rootSelectorShapes"`
	ComposingRoots      []string            `json:"composingRoots"`
	ColorNames          []string            `json:"colorNames"`
	StaticProperties    []utilityProperties `json:"staticProperties"`
}

// rootSelectorShape is a root whose utilities emit under something other than a bare class selector.
type rootSelectorShape struct {
	Root  string `json:"root"`
	Shape string `json:"shape"`
}

// utilityProperties is one utility and the CSS property names it declares.
type utilityProperties struct {
	Root       string   `json:"root"`
	Properties []string `json:"properties"`
}

// family is one collapse: two roots that merge into a third when everything else about them agrees.
type family struct {
	Inputs            []string `json:"inputs"`
	Output            string   `json:"output"`
	DiscoveredAtValue string   `json:"discoveredAtValue"`
}

func main() {
	entryPoint := flag.String("entry-point", "", "path to the Tailwind CSS entry point, such as app/_theme/styles/theme.css")
	output := flag.String("output", "internal/tailwind/collapse_table.go", "where to write the generated table")
	check := flag.Bool("check", false, "regenerate and fail if the committed table disagrees, instead of writing it")
	flag.Parse()

	if *entryPoint == "" {
		fmt.Fprintln(os.Stderr, "gen_tailwind_collapse: -entry-point is required")
		fmt.Fprintln(os.Stderr, "  example: go run ./tools/gen_tailwind_collapse -entry-point ~/Projects/ahra/app/_theme/styles/theme.css")
		os.Exit(2)
	}

	result, err := enumerate(*entryPoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen_tailwind_collapse: %v\n", err)
		os.Exit(1)
	}

	// A run that discovered nothing is the failure this generator is most likely to have, and it
	// produces a table that looks clean. `canonicalizeCandidates` returns its input unchanged when
	// called without `collapse: true`, so an options mistake enumerates zero families and writes an
	// empty table that no test would obviously reject.
	if len(result.Families) == 0 {
		fmt.Fprintln(os.Stderr, "gen_tailwind_collapse: the engine reported no collapse families at all.")
		fmt.Fprintln(os.Stderr, "  That is almost certainly a broken enumeration rather than a Tailwind with no shorthands.")
		fmt.Fprintln(os.Stderr, "  Check that canonicalizeCandidates is being called with {collapse: true}.")
		os.Exit(1)
	}
	if result.FunctionalRoots < 100 {
		fmt.Fprintf(os.Stderr, "gen_tailwind_collapse: only %d functional roots discovered, which is too few to be the real registry.\n", result.FunctionalRoots)
		fmt.Fprintln(os.Stderr, "  A table built from a partial registry is silently missing families.")
		os.Exit(1)
	}

	rendered, err := render(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen_tailwind_collapse: %v\n", err)
		os.Exit(1)
	}

	if *check {
		committed, err := os.ReadFile(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gen_tailwind_collapse: cannot read %s to check it: %v\n", *output, err)
			os.Exit(1)
		}
		if !bytes.Equal(committed, rendered) {
			fmt.Fprintf(os.Stderr, "gen_tailwind_collapse: %s is stale for Tailwind %s.\n", *output, result.TailwindVersion)
			fmt.Fprintln(os.Stderr, "  The installed Tailwind collapses a different set of families than the committed table records.")
			fmt.Fprintln(os.Stderr, "  Regenerate it and read the diff: what changed is what this upgrade changed about canonical class names.")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "gen_tailwind_collapse: %s is current for Tailwind %s (%d families).\n",
			*output, result.TailwindVersion, len(result.Families))
		return
	}

	if err := os.WriteFile(*output, rendered, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen_tailwind_collapse: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "gen_tailwind_collapse: wrote %s — Tailwind %s, %d families from %d pairs over %d roots.\n",
		*output, result.TailwindVersion, len(result.Families), result.PairsProbed, result.FunctionalRoots)
}

// enumerate runs the Node script that asks the real engine.
//
// Node's stderr is passed through rather than captured, so a resolution failure inside the script
// reaches the operator instead of being swallowed into a parse error about empty input.
func enumerate(entryPoint string) (*enumeration, error) {
	absoluteEntryPoint, err := filepath.Abs(entryPoint)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", entryPoint, err)
	}
	if _, err := os.Stat(absoluteEntryPoint); err != nil {
		return nil, fmt.Errorf("entry point %s is not readable: %w", absoluteEntryPoint, err)
	}

	scriptPath, err := scriptBesideThisTool()
	if err != nil {
		return nil, err
	}

	command := exec.Command("node", scriptPath, absoluteEntryPoint)
	command.Stderr = os.Stderr

	stdout, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("running the enumeration: %w", err)
	}

	var result enumeration
	if err := json.Unmarshal(stdout, &result); err != nil {
		return nil, fmt.Errorf("parsing the enumeration: %w", err)
	}
	return &result, nil
}

// scriptBesideThisTool finds enumerate.mjs relative to the repository rather than the caller.
//
// The tool is run with `go run ./tools/gen_tailwind_collapse` from the repository root, so the
// script sits at a known path from there. Locating it by walking up from the working directory
// would silently pick up a different checkout when run from a subdirectory.
func scriptBesideThisTool() (string, error) {
	candidate := filepath.Join("tools", "gen_tailwind_collapse", "enumerate.mjs")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("cannot find %s; run this from the repository root", candidate)
}

// render writes the Go source for the table.
//
// Gofmt is applied here rather than left to the caller so that `-check` compares bytes that a
// formatter cannot later change, which is what makes the check a real gate rather than a source of
// spurious failures.
func render(result *enumeration) ([]byte, error) {
	var buffer bytes.Buffer

	fmt.Fprintf(&buffer, `// Code generated by tools/gen_tailwind_collapse. DO NOT EDIT.
//
// Regenerate with:
//
//	go run ./tools/gen_tailwind_collapse -entry-point <theme.css>
//
// Source: Tailwind %s, enumerated from %s.
// %d functional roots produced %d candidate pairs, of which %d collapse.
//
// Each entry is a fact about roots rather than about classes: %s
// holds for every value the two roots share, so the table is small and does not grow with the
// codebase. Enumerated from the design system's own class list rather than from any codebase's
// usage, because a table built from observed classes is silently missing every family nobody has
// written yet.

package tailwind

// CollapseFamily is two utility roots that merge into a third.
//
// The merge only happens when everything except the root already agrees between the two classes:
// their variants, their importance, and their value. That precondition is the gate's job; this
// table answers only "if those match, do these two roots have a shorthand".
type CollapseFamily struct {
	First  string
	Second string
	Output string
}

// CollapseFamilies is every family the installed Tailwind knows, sorted by input roots.
var CollapseFamilies = []CollapseFamily{
`,
		result.TailwindVersion,
		result.EntryPoint,
		result.FunctionalRoots,
		result.PairsProbed,
		len(result.Families),
		exampleFamily(result.Families),
	)

	for _, entry := range result.Families {
		if len(entry.Inputs) != 2 {
			return nil, fmt.Errorf("family %v does not have exactly two inputs", entry.Inputs)
		}
		fmt.Fprintf(&buffer, "\t{First: %q, Second: %q, Output: %q},\n", entry.Inputs[0], entry.Inputs[1], entry.Output)
	}

	fmt.Fprintf(&buffer, `}

// RootDeclaredProperties is the CSS property names each functional utility root declares.
//
// Two classes conflict when they declare the same property, which is what no-conflicting-classes
// reports. Property names rather than values, and the distinction inverts both answers: w-8 and
// h-8 declare the same value under different properties and do not conflict, while px-4 and px-8
// declare different values under one property and do.
//
// Shorthands are deliberately not normalised. padding and padding-inline are different names, and
// upstream reports no conflict between p-4 and px-8 even though they visually overlap.
var RootDeclaredProperties = map[string][]string{
`)
	for _, entry := range result.RootProperties {
		fmt.Fprintf(&buffer, "\t%q: {%s},\n", entry.Root, quotedList(entry.Properties))
	}

	fmt.Fprintf(&buffer, `}

// RootColorProperties is what a root declares when its value is a color rather than a length.
//
// border is border-width plus border-style with a number and border-color with a color, so border
// and border-neutral-200 declare different things despite sharing a root. Reporting them as
// conflicting produced 347 findings on a tree whose real count is zero.
//
// Recorded per root rather than per class: the exceptions are the color scale multiplied by the
// roots that accept it, which is 7,854 entries resolving to 14 distinct property sets.
var RootColorProperties = map[string][]string{
`)
	for _, entry := range result.RootColorProperties {
		fmt.Fprintf(&buffer, "\t%q: {%s},\n", entry.Root, quotedList(entry.Properties))
	}

	fmt.Fprintf(&buffer, `}

// RootSelectorShapes is the selector a root's utilities emit under, when it is not a bare class.
//
// divide-neutral-200 and border-neutral-200 both declare border-color and do not conflict, because
// divide emits under :where(.CLASS > :not(:last-child)) and targets child elements while border
// emits on the element itself. Two classes can only collide when they land on the same element.
//
// Only non-default shapes are stored: a root absent here emits under a bare .CLASS selector.
var RootSelectorShapes = map[string]string{
`)
	for _, entry := range result.RootSelectorShapes {
		fmt.Fprintf(&buffer, "\t%q: %q,\n", entry.Root, entry.Shape)
	}

	fmt.Fprintf(&buffer, `}

// ComposingRoots are roots whose utilities layer rather than overwrite each other.
//
// shadow-lg and ring-1 both declare box-shadow and do not conflict: every shadow and every ring
// emits the same var() chain and each contributes through its own custom property. px-4 and px-8
// declare different values under one property and genuinely collide.
//
// Detected by asking whether two different values of a root produce identical declaration text.
var ComposingRoots = map[string]bool{
`)
	for _, root := range result.ComposingRoots {
		fmt.Fprintf(&buffer, "\t%q: true,\n", root)
	}

	fmt.Fprintf(&buffer, `}

// ColorNames is every color value the design system defines.
//
// Needed because deciding whether a class value is a color requires the theme's palette, which is
// the theme-dependent knowledge a Go-side table exists to carry. Hardcoding Tailwind's default
// palette would be wrong for any project that customises it.
var ColorNames = map[string]bool{
`)
	for _, name := range result.ColorNames {
		fmt.Fprintf(&buffer, "\t%q: true,\n", name)
	}

	fmt.Fprintf(&buffer, `}

// StaticDeclaredProperties is the same, for utilities whose whole name is their identity.
var StaticDeclaredProperties = map[string][]string{
`)
	for _, entry := range result.StaticProperties {
		fmt.Fprintf(&buffer, "\t%q: {%s},\n", entry.Root, quotedList(entry.Properties))
	}

	fmt.Fprintf(&buffer, `}

// TailwindVersion is the version these families were enumerated from.
//
// Recorded so a mismatch between this and the installed Tailwind is greppable, and so a reader of a
// finding can tell which engine's opinion produced it.
const TailwindVersion = %q
`, result.TailwindVersion)

	formatted, err := format.Source(buffer.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting the generated table: %w", err)
	}
	return formatted, nil
}

// quotedList renders a string slice as Go literal elements.
func quotedList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	return strings.Join(quoted, ", ")
}

// exampleFamily names a real entry in the doc comment, so the file explains itself with something
// from this Tailwind rather than with a hardcoded example that may no longer be true.
func exampleFamily(families []family) string {
	for _, entry := range families {
		if len(entry.Inputs) == 2 && entry.Inputs[0] == "px" && entry.Inputs[1] == "py" {
			return "`px + py => p`"
		}
	}
	if len(families) > 0 && len(families[0].Inputs) == 2 {
		return "`" + strings.Join(families[0].Inputs, " + ") + " => " + families[0].Output + "`"
	}
	return "a family"
}
