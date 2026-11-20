package guard

import (
	"slices"
	"strings"
	"testing"
)

// checkPackage is the Go engine's core, the lint phase every binary that runs or tests the engine links.
const checkPackage = "github.com/system-inc/cohere/internal/go_engine/check"

// goEngineRules is the Go engine's registry, whose imports are every package that registers a rule, and the prefix
// every package under it shares.
const goEngineRules = "github.com/system-inc/cohere/internal/go_engine/rules"

// The engine's core links no rule package (#f5g7ym6). check took stage 5's presence reader by importing
// standing_presence, which imports consistency_no_any, so every binary that linked check made consistency-no-any's
// listener as its variables were made, and that listener heard every walk of the shared dispatcher, typeless ones
// included. The reader is an option now (check.Options.FactReachPresence), and this holds that nothing check links
// outside its tests, read transitively, is a rule package: one the registry imports, read from the registry's own
// imports so a rule package added later is covered without an edit here.
//
// check still links four packages under the registry's directory, none a rule: dispatch, the two dispatchers every
// dispatched rule's analyzer requires, which check tolerates and schedules; build_intermediate_representation, the
// engine's own intermediate representation, which check schedules beside them; and positions and marker, the line
// table and the marker comments, which internal/go_engine/rule imports to answer a syntax-only context. Each is
// machinery a rule runs on and registers nothing, so linking one puts no listener on any walk.
//
// A registry that imports too few packages to be every rule's, or a listing that holds no check, fails rather than
// reading clean.
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
	linked := linkedPackages(test, "./internal/go_engine/check")
	if !slices.Contains(linked, checkPackage) {
		test.Fatalf("go list names no %s among what it links, so the listing read nothing", checkPackage)
	}
	for _, importPath := range linked {
		if !registered[importPath] {
			continue
		}
		test.Errorf("check links %s, a rule package the registry imports, and its registration rides into every "+
			"binary that links check: take what check needs of it as an option of check.Options, as "+
			"FactReachPresence takes standing_presence's reader; find the importer with "+
			"`go list -deps -f '{{.ImportPath}} {{.Imports}}' ./internal/go_engine/check`", importPath)
	}
}
