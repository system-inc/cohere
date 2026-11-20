package guard

import (
	"slices"
	"strings"
	"testing"
)

// checkPackage is the Go engine's core, the lint phase every binary that runs or tests the engine links.
const checkPackage = "github.com/system-inc/cohere/internal/go_engine/check"

// goEngineRules is the Go engine's registry, which imports every package that registers a rule, and the prefix every
// package under it shares.
const goEngineRules = "github.com/system-inc/cohere/internal/go_engine/rules"

// checkRulesMachinery are the packages under the registry's directory the engine's core may link: the machinery a rule
// runs on, which registers no rule of its own. A rule package's analyzer registers its listener on the shared
// dispatcher as its variables are made, so linking one puts its listener on every walk of the binary, the edge
// consistency-no-any rode into vet/unreachable's typeless parity walk on.
var checkRulesMachinery = map[string]bool{
	// The two dispatchers every dispatched rule's analyzer requires, which check tolerates and schedules.
	goEngineRules + "/dispatch": true,
	// The engine's own intermediate representation, which check schedules beside the dispatchers.
	goEngineRules + "/build_intermediate_representation": true,
	// The line table and the marker comments the dispatcher reads, which register nothing.
	goEngineRules + "/positions": true,
	goEngineRules + "/marker":    true,
}

// The engine's core links no rule package (#f5g7ym6). check took stage 5's presence reader by importing
// standing_presence, which imports consistency_no_any, so every binary that linked check made consistency-no-any's
// listener at init and heard every walk of the shared dispatcher, typeless ones included. The reader is an option now
// (check.Options.FactReachPresence), and this holds that no package check links, outside its tests, is one under the
// registry's directory beyond the machinery a rule runs on, nor one the registry names. A listing that found no
// registered rule, or machinery the registry names, fails rather than reading clean.
func TestTheEnginesCoreLinksNoRulePackage(test *testing.T) {
	test.Parallel()
	_, imports := listImports(test, "./internal/go_engine/rules")
	registered := map[string]bool{}
	for _, imported := range imports[goEngineRules] {
		if strings.HasPrefix(imported, goEngineRules+"/") {
			registered[imported] = true
		}
	}
	if len(registered) < 100 {
		test.Fatalf("the registry imports %d packages under it, too few to be every rule's", len(registered))
	}
	for machinery := range checkRulesMachinery {
		if registered[machinery] {
			test.Errorf("the registry imports %s, so it registers rules and is no machinery check may link", machinery)
		}
	}
	linked := linkedPackages(test, "./internal/go_engine/check")
	if !slices.Contains(linked, checkPackage) {
		test.Fatalf("go list names no %s among what it links, so the listing read nothing", checkPackage)
	}
	for _, importPath := range linked {
		if !strings.HasPrefix(importPath, goEngineRules+"/") || checkRulesMachinery[importPath] {
			continue
		}
		test.Errorf("check links %s, a package under the registry's directory, and a rule package's registration "+
			"rides into every binary that links check: take what it needs as an option of check.Options, as "+
			"FactReachPresence takes standing_presence's reader; find the importer with "+
			"`go list -deps -f '{{.ImportPath}} {{.Imports}}' ./internal/go_engine/check`", importPath)
	}
}
