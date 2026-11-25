package go_language

import (
	"cmp"
	"maps"
	"path/filepath"
	"strings"
	"testing"
)

// A named array's length folds into a constant in the reading body's own types, so a body that reads the length of a
// type declared apart can differ between builds though nothing it names is declared differently in its own file
// (#bed0mnk, Rules's question on #zvwfe26). The body-reach-array-length fixture's Block is `[97]byte` on the host's
// platform and `[]byte` on windows, and Counted `[97]byte` against `[13]byte`, a constant's length needing an array on
// every platform. Each body reads a length one way: `len`, `cap` and `unsafe.Sizeof` of a literal, `len` of a
// variable another file declares, and a constant folded from a variable's length, in arrays itself and again across
// the package boundary in user. The reach carries each: every such body is reached, so the body differential reads no
// failure, comparing Plain alone in each package, a length no build changes. A body the reach left unreached would
// fail there by its types, each length's constant differing.
func TestTheReachCarriesANamedArraysLengthIntoEveryBodyThatFoldsIt(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach-array-length"))
	on, _ := runOverConfigured(test, root, exitClean, func(pipeline *pipeline) {
		pipeline.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		pipeline.bodyDifferential = true
	}, "--no-cache", "--no-fix", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
	}
	census, differential := on.buildRuns[1].bodyReach, on.buildRuns[1].bodyDifferential
	if census == nil || differential == nil {
		test.Fatal("windows's run has no census or no body differential")
	}
	got := map[string]string{}
	for _, body := range census.bodies {
		if body.packageIdentifier == "example.com/arrays" || body.packageIdentifier == "example.com/arrays/user" {
			name := strings.TrimPrefix(body.packageIdentifier, "example.com/") + "." + body.name
			got[name] = cmp.Or(string(body.reason), "unreached")
			test.Logf("%s: %s, %s", name, got[name], strings.ReplaceAll(body.driver, root+string(filepath.Separator),
				""))
		}
	}
	// Of the three specs, Length's alone has a body, Measured's length, reached by Measured's name. Across the
	// boundary, Variable is reached first by Held's type, which the use walk records beside Held itself.
	want := map[string]string{
		"arrays.Length":        "Named",
		"arrays.Plain":         "unreached",
		"arrays/user.Plain":    "unreached",
		"arrays.Literal":       "Named",
		"arrays.Capacity":      "Named",
		"arrays.Size":          "Named",
		"arrays.Variable":      "Named",
		"arrays.Constant":      "Constant",
		"arrays/user.Literal":  "Named",
		"arrays/user.Capacity": "Named",
		"arrays/user.Size":     "Named",
		"arrays/user.Variable": "ValueType",
		"arrays/user.Constant": "Constant",
	}
	if !maps.Equal(got, want) {
		test.Errorf("windows's census judges the bodies that read a length\n%v\nwant\n%v", got, want)
	}
	var failures []string
	for _, failure := range differential.failures {
		failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
	}
	if len(failures) > 0 {
		test.Errorf("want the body differential to read no failure, got\n%s", strings.Join(failures, "\n"))
	}
}
