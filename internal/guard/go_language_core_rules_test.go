package guard

import (
	"slices"
	"strings"
	"testing"
)

// checkPackage is cohere-go's core, the lint phase every binary that runs or tests the language links.
const checkPackage = "github.com/system-inc/cohere/internal/go_language/check"

// goLanguageRules is cohere-go's registry, which links every package that registers a rule, and the prefix every
// package under it shares.
const goLanguageRules = "github.com/system-inc/cohere/internal/go_language/rules"

// goLanguageDispatch is the shared dispatcher every dispatched rule registers its listener on.
const goLanguageDispatch = goLanguageRules + "/dispatch"

// The language's core links no rule package (#f5g7ym6). check took stage 5's presence reader by importing
// standing_presence, which imports consistency_no_any, so every binary that linked check made consistency-no-any's
// listener as its variables were made, and that listener heard every walk of the shared dispatcher, typeless ones
// included. The reader is an option now (check.Options.FactReachPresence), and this holds that nothing check links
// outside its tests, read transitively, is a rule package.
//
// A rule package is one under the registry's directory that the registry links, read transitively from the
// registry's own imports, so a rule the registry reaches only through a group package is one as much as a rule it
// names, and one added later is covered without an edit here. The machinery a rule runs on is what the dispatcher
// itself links: dispatch, build_intermediate_representation, positions and marker today. check links those four,
// and none can register a listener, since registering one means importing dispatch, and a package dispatch links
// that imported dispatch would be an import cycle.
//
// A registry that links too few packages to be every rule's, or a listing that holds no check, fails rather than
// reading clean.
func TestTheGoLanguageCoreLinksNoRulePackage(test *testing.T) {
	test.Parallel()
	machinery := linkedPackages(test, "./internal/go_language/rules/dispatch")
	if !slices.Contains(machinery, goLanguageDispatch) {
		test.Fatalf("go list names no %s among what it links, so the listing read nothing", goLanguageDispatch)
	}
	registered := map[string]bool{}
	for _, importPath := range linkedPackages(test, "./internal/go_language/rules") {
		if strings.HasPrefix(importPath, goLanguageRules+"/") && !slices.Contains(machinery, importPath) {
			registered[importPath] = true
		}
	}
	if len(registered) < 100 {
		test.Fatalf("the registry links %d rule packages under it, too few to be every rule's", len(registered))
	}
	linked := linkedPackages(test, "./internal/go_language/check")
	if !slices.Contains(linked, checkPackage) {
		test.Fatalf("go list names no %s among what it links, so the listing read nothing", checkPackage)
	}
	for _, importPath := range linked {
		if !registered[importPath] {
			continue
		}
		test.Errorf("check links %s, a rule package the registry links, and its registration rides into every "+
			"binary that links check: take what check needs of it as an option of check.Options, as "+
			"FactReachPresence takes standing_presence's reader; find the importer with "+
			"`go list -deps -f '{{.ImportPath}} {{.Imports}}' ./internal/go_language/check`", importPath)
	}
}
