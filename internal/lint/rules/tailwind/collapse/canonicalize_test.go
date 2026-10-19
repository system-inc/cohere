package tailwind

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// canonicalSinglesFixture is what generate_canonical_singles records: Tailwind's own answer for every
// class, one class at a time.
type canonicalSinglesFixture struct {
	TailwindVersion string `json:"tailwindVersion"`
	EntryPoint      string `json:"entryPoint"`
	Cases           []struct {
		ClassName string `json:"className"`
		Origin    string `json:"origin"`
		Canonical string `json:"canonical"`
	} `json:"cases"`
}

// canonicalUnportedFixture is the committed list of classes Tailwind rewrites and this port leaves alone,
// by the upstream pass that rewrites them. A class leaves the list in the change that ports its pass.
type canonicalUnportedFixture map[string][]string

// TestCanonicalizeAgreesWithTailwind holds the port to Tailwind's canonicalizer, class by class, on the
// public theme and on ahra's (#15swfzq).
//
// Three outcomes per class, and only two are allowed. The port gives Tailwind's answer. Or Tailwind
// rewrites the class and the port gives something else, because a pass that rewrites it is not ported:
// the port leaves it as written, or printing changes it and the missing pass would have changed it
// further. Then the class must be in canonical_unported.json under that pass, so a shortfall is always
// counted and never assumed. The third outcome, a rewrite of a class Tailwind leaves alone, is the one
// this rule exists to never produce, and it fails with no list to hide in.
//
// The list is held exact in both directions: a class that is listed and now agrees fails too, so porting
// a pass shrinks the list in the same change, and the count it prints is the true remainder.
func TestCanonicalizeAgreesWithTailwind(t *testing.T) {
	t.Parallel()
	forEachOracleCopy(t, "testdata/canonical_singles.json", func(t *testing.T, fixtureName string) {
		var fixture canonicalSinglesFixture
		readJsonFixture(t, fixtureName, &fixture)
		if len(fixture.Cases) == 0 {
			t.Fatalf("%s holds no cases, so agreement would be vacuous", fixtureName)
		}

		unportedName := oracleSibling(t, fixtureName, "testdata/canonical_unported.json")
		var unported canonicalUnportedFixture
		readJsonFixture(t, unportedName, &unported)
		passByClass := map[string]string{}
		for pass, classes := range unported {
			for _, className := range classes {
				passByClass[className] = pass
			}
		}

		canonicalizer := NewCanonicalizer(oracleSystem(t, fixture.EntryPoint))

		agreed, rewrites, silent := 0, 0, map[string][]string{}
		var unlisted []string
		for _, testCase := range fixture.Cases {
			got := canonicalizer.CanonicalizeClass(testCase.ClassName)
			if testCase.Canonical != testCase.ClassName {
				rewrites++
			}
			switch {
			case got == testCase.Canonical:
				agreed++
				if pass, isListed := passByClass[testCase.ClassName]; isListed {
					t.Errorf("%s agrees with Tailwind now (%q), so it leaves %s's %s list",
						testCase.ClassName, got, unportedName, pass)
				}
			case testCase.Canonical == testCase.ClassName:
				t.Errorf("%s (%s): Tailwind leaves it as written and the port gives %q, a rewrite Tailwind "+
					"does not make", testCase.ClassName, testCase.Origin, got)
			default:
				// Tailwind rewrites it and the port either leaves it or stops partway, where printing
				// changed it and an unported pass would have changed it further. Both report the class, so
				// neither is an over-report, but the partial one names the wrong replacement, and either way
				// it is counted under the pass that would finish it.
				pass, isListed := passByClass[testCase.ClassName]
				if !isListed {
					unlisted = append(unlisted, testCase.ClassName+" ("+testCase.Origin+") -> Tailwind "+
						testCase.Canonical+", port "+got)
					continue
				}
				silent[pass] = append(silent[pass], testCase.ClassName)
			}
		}

		if len(unlisted) > 0 {
			sort.Strings(unlisted)
			t.Errorf("%d classes Tailwind rewrites are left as written and not listed in %s, under the pass "+
				"that rewrites them:\n  %s", len(unlisted), unportedName, strings.Join(unlisted, "\n  "))
		}

		seen := map[string]bool{}
		for _, testCase := range fixture.Cases {
			seen[testCase.ClassName] = true
		}
		for className, pass := range passByClass {
			if !seen[className] {
				t.Errorf("%s lists %s under %s, and %s has no such class", unportedName, className, pass, fixtureName)
			}
		}

		passes := make([]string, 0, len(silent))
		silentCount := 0
		for pass, classes := range silent {
			passes = append(passes, pass)
			silentCount += len(classes)
		}
		sort.Strings(passes)
		t.Logf("%s: %d classes, %d rewritten by Tailwind, %d answered alike, %d short of Tailwind by unported passes",
			fixtureName, len(fixture.Cases), rewrites, agreed, silentCount)
		for _, pass := range passes {
			t.Logf("  %s: %d", pass, len(silent[pass]))
		}
	})
}

// readJsonFixture decodes a committed fixture, failing the test rather than skipping when it is absent,
// because an oracle that silently goes missing is a test that silently stops comparing.
func readJsonFixture(t *testing.T, name string, into any) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Clean(name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	if err := json.Unmarshal(contents, into); err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
}
