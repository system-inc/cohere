package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Parity is the acceptance criterion for this whole tool, and until now nothing measured it.
//
// `rule-inventory.json` is the superset of every rule the two tools being replaced enforce. It was
// written once by hand and read by nothing: no test, no gate, no code path referenced it. So the
// one number the project is judged on was a number somebody recomputed by hand and got wrong at
// least four times in one night, always in the direction of sounding further along.
//
// It says *enforce* rather than *list*, and the difference was worth 40 rules. An earlier inventory
// was built from the rules written down in a config, which cannot see a rule an oxlint `plugins`
// declaration turns on by default. Forty were firing on this tree and named in no rules block
// anywhere, proven by planting violations and watching `no-const-assign` and `react/no-children-prop`
// report. Every count taken before that read configuration and was therefore blind to them by
// construction.
//
// So an entry carries `enabledBy`, which is `rulesBlock` for a rule somebody wrote down and
// `pluginDefault` for one that arrives without being named. A future pass adding a surface should
// add a value here rather than quietly folding it into the first.
//
// An entry also carries `severity`, and the reason is that all forty are `warn` rather than `error`.
// A warning does not fail the gate today, and parity is still about what verify must be able to
// see: severity is a config decision that changes in an afternoon, while whether a rule exists in
// the catalog is a build decision that takes a day. Recording it means raising those forty to error
// later is a config change rather than a porting project.
//
// The failure this closes is the one this tool exists to prevent, arriving through its own front
// door. A run with every rule ported and a run with a hundred missing look identical: the tree goes
// green either way, because the tool being replaced has been rejecting those violations for months.
// A clean result is evidence of enforcement only if something independently states what was
// supposed to be enforced.
//
// This test therefore never fails on an incomplete port. Failing would make the gate red for weeks
// and get the guard deleted, and an unported rule is a known state rather than a defect. It fails
// only when the inventory and the registry disagree in ways that mean one of them is wrong: a
// registered rule the inventory has never heard of, or an inventory that cannot be read at all.
// The remaining count is reported with `-v` and written to a file the dashboard can read.
func TestParityAgainstInventory(t *testing.T) {
	inventory := readInventory(t)
	registered := registeredRuleNames()

	if len(registered) == 0 {
		// A sweep with nothing to compare passes for the wrong reason, which is the same shape as
		// the defect it guards against.
		t.Fatal("registry.All() returned no rules, so this test compared nothing")
	}

	var ported, remaining []string
	claimed := make(map[string]bool, len(registered))

	var declined []string
	for _, entry := range inventory {
		if name, found := matchRegistered(entry, registered); found {
			claimed[name] = true
			ported = append(ported, entry)
			continue
		}
		if _, known := rulesDeclined[entry]; known {
			declined = append(declined, entry)
			continue
		}
		remaining = append(remaining, entry)
	}

	// A registered rule matching no inventory entry is the one direction that is a real defect. It
	// means we are enforcing a judgment the gate we are replacing does not enforce, so the
	// differential would read it as a disagreement and nobody would know which side was right.
	for _, name := range registered {
		if claimed[name] {
			continue
		}
		if reason, known := rulesOutsideTheInventory[name]; known {
			t.Logf("rule %q is registered and outside the inventory on purpose: %s", name, reason)
			continue
		}
		t.Errorf("rule %q is registered and appears in no inventory entry, so nothing states "+
			"whether the gate being replaced enforces it; either the inventory is stale or "+
			"this rule enforces something we never agreed to", name)
	}

	sort.Strings(remaining)
	sort.Strings(declined)
	for _, entry := range declined {
		t.Logf("rule %q is declined rather than unported: %s", entry, rulesDeclined[entry])
	}
	t.Logf("parity: %d of %d (%d remaining, %d declined)",
		len(ported), len(inventory), len(remaining), len(declined))
	for _, namespace := range namespacesOf(remaining) {
		t.Logf("  %-20s %d left", namespace.name, namespace.count)
	}
}

// TestRegisteredNamesAreBare refuses a namespaced registration, which parity cannot see.
//
// `matchRegistered` tries `entry == name` first. An inventory entry `react/unsupported-syntax` and a
// rule registered under that same slash spelling match on that branch exactly, so parity credits the
// rule and reports the identical count either way. The mistake is invisible in the one number the
// project is judged on, which is worse than a mistake the number reports.
//
// Five porters measured this area independently and each reported that the slash spelling survives
// all three guards. It does, and survival was not the finding: the sixth measured what a reader
// actually sees and found the count could not distinguish the two spellings at all.
//
// The rule is bare, and the reason is not taste. `verify --rules` prints names with no namespace,
// every rule in the catalog registers bare, and `config.settingFor` performs the same split on the
// `/` boundary when it resolves an inventory entry against a registration. A namespaced name is
// therefore inconsistent with its siblings in output a user reads, and the only signal that would
// have caught it is a number that cannot.
//
// Green when it lands, and that is deliberate rather than a weakness: nothing registers namespaced
// today. It exists so the next rule that does is refused by name at the moment it is written, rather
// than counted as ported and found by the seventh person to measure this area.
//
// # Reproducing the evidence, and not the way it was first produced
//
// A guard that has never failed is worth nothing, so this one was checked by planting
// `Name: "react/unsupported-syntax"` in `internal/rules/react/unsupported_syntax.go`. Both guards
// were then run: parity reported `213 of 214` and passed, unchanged by the mutation, while this test
// failed and named the rule. That pair is the whole argument -- the count cannot see the mistake and
// this can.
//
// The mutation was installed by copying a backup over the file and copying it back, which is the
// two-step blind write the domain body forbids for exactly this tree. Nothing was lost, and a peer
// committed twice inside the window, so that is ordering luck rather than safety.
//
// The safe way to repeat it is to not write into the shared tree at all: ask the file's owner to run
// the mutation, or copy the package elsewhere and mutate the copy. If a reference to the original is
// needed for comparison, take it from the committed blob rather than from a backup, because the blob
// is immutable and a backup is a write waiting to happen:
//
//	git show HEAD:internal/rules/react/unsupported_syntax.go > "$scratch/original.go"
//
// That form answers "what does this file contain at this commit", and it is the right question when
// capturing a baseline. It is emphatically not an answer to "what did this commit change" -- a
// commit inherits every line from its ancestors, so `git show <sha>:<path>` happily returns content
// from a file that commit never touched. A wrong sha was confirmed that way in this repository: the
// probe ran, reported honestly about the wrong object, and agreed with the story being checked.
// For what a commit changed, `git show --stat <sha>` or `git log <sha> -- <path>`.
func TestRegisteredNamesAreBare(t *testing.T) {
	registered := registeredRuleNames()
	if len(registered) == 0 {
		t.Fatal("registry.All() returned no rules, so this test checked nothing")
	}

	for _, name := range registered {
		if !strings.Contains(name, "/") {
			continue
		}
		bare := name[strings.LastIndex(name, "/")+1:]
		t.Errorf("rule %q is registered with a namespace; register it as %q instead. The parity "+
			"guard counts both spellings the same, so this would have been credited as ported "+
			"while `verify --rules` printed it inconsistently with every other rule", name, bare)
	}
}

// readInventory returns every rule name the inventory lists.
//
// The path is resolved from this file's package directory rather than the working directory,
// because `go test ./...` runs each package in its own directory and a relative path that works
// from the repository root silently reads nothing from here.
func readInventory(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("..", "..", "rule-inventory.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		// A missing inventory is a hard failure rather than a skip. A skipped parity check reads
		// exactly like a passing one in a log, and this is the number the project is judged on.
		t.Fatalf("cannot read %s: %v", path, err)
	}

	var document struct {
		Rules []struct {
			Rule string `json:"rule"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	if len(document.Rules) == 0 {
		t.Fatalf("%s parsed to zero rules, so the comparison would pass by having nothing to compare", path)
	}

	names := make([]string, 0, len(document.Rules))
	for _, entry := range document.Rules {
		names = append(names, entry.Rule)
	}
	return names
}

// registeredRuleNames returns the name of every rule in All().
func registeredRuleNames() []string {
	rules := All()
	names := make([]string, 0, len(rules))
	for _, registered := range rules {
		names = append(names, registered.Name)
	}
	return names
}

// matchRegistered reports which registered rule an inventory entry names, if any.
//
// The inventory writes `nextjs/no-img-element` and the registry writes `no-img-element`, the same
// split `config.settingFor` resolves. The suffix must fall on a `/` boundary for the same reason it
// does there: plain suffix matching would let `no-enum` claim `consistency-no-enum`, which is a rule
// nobody named, and the count would read one higher than the truth.
func matchRegistered(entry string, registered []string) (string, bool) {
	for _, name := range registered {
		if entry == name {
			return name, true
		}
		if prefix := strings.TrimSuffix(entry, name); prefix != entry && strings.HasSuffix(prefix, "/") {
			return name, true
		}
	}
	return "", false
}

// namespaceCount is one namespace and how many of its rules are still unported.
type namespaceCount struct {
	name  string
	count int
}

// namespacesOf groups unported rule names by their inventory namespace, largest block first.
//
// An entry with no `/` is a core rule, which the inventory writes bare and which the byNamespace
// block of that file calls `(core)`.
func namespacesOf(remaining []string) []namespaceCount {
	counts := map[string]int{}
	for _, entry := range remaining {
		namespace := "(core)"
		if index := strings.Index(entry, "/"); index >= 0 {
			namespace = entry[:index]
		}
		counts[namespace]++
	}

	ordered := make([]namespaceCount, 0, len(counts))
	for name, count := range counts {
		ordered = append(ordered, namespaceCount{name: name, count: count})
	}
	sort.Slice(ordered, func(first, second int) bool {
		if ordered[first].count != ordered[second].count {
			return ordered[first].count > ordered[second].count
		}
		return ordered[first].name < ordered[second].name
	})
	return ordered
}

// rulesOutsideTheInventory names every registered rule the gate being replaced does not enforce,
// and why that is a decision rather than a mistake.
//
// It is compiled in rather than read from a file, for the same reason the differential's
// acknowledged differences are: an exemption that can be supplied at the call site is an exemption
// nobody reviews, and this map is the one place a rule can be exempt from the project's own
// acceptance criterion.
//
// A reason is required. An entry here says "we looked", and an entry with no reason says only that
// somebody wanted the test to pass.
var rulesOutsideTheInventory = map[string]string{
	// Registered in the nexus plugin map at NexusLintConfiguration.ts:31 and enabled in no rules
	// block, which that file's own comment names as inert: the plugin knows the name and nothing
	// turns it on. Confirmed by a whole-tree search returning exactly one occurrence, the plugin
	// map line. So the rule is real, its judgment is real, and it has never run anywhere. Porting
	// it was correct and enabling it is a decision for Kirk rather than a parity question.
	"import-require-path-alias": "in the nexus plugin map, enabled in no config, so it has never run",

	// A house rule with no upstream on either side, written from reasoning rather than ported, so
	// no inventory entry could exist for it. It guards verify's own type-based React rules rather
	// than the source: in a file where a hook call resolves to `any`, `set-state-in-render` and
	// `set-state-in-effect` report nothing and nothing says a check was skipped. The gate being
	// replaced has no equivalent because it has no such rules to protect.
	"react-hook-any-type": "a house tripwire over verify's own type-based React rules; the gate " +
		"being replaced has no rule it could correspond to",
}

// rulesDeclined names every inventory rule we have decided not to port, and why.
//
// This is a fourth state beside ported, remaining, and cancelled, and it exists because the other
// three cannot say "we looked at this and chose not to". A declined rule counted as remaining reads
// as work nobody has got to yet, and a declined rule quietly dropped from the inventory reads as a
// rule that was never enforced. Both lose the decision.
//
// The bar for an entry is the same as for `rulesOutsideTheInventory`: a reason a future reader can
// act on. Specifically, what supersedes the rule, and what the decline costs if that thing ever goes
// away, because a decline that holds today may not hold for a different consumer.
// The map is empty, and that is a state rather than an oversight. Its one entry declined
// `nextjs/no-html-link-for-pages` on the ground that `structure/react-no-anchor-element` was
// strictly stronger, and that premise was measured false and the rule ported. Keeping the mechanism
// with nothing in it costs a reader nothing and means the next decline is an entry rather than a
// rediscovery of why this state exists.
var rulesDeclined = map[string]string{}

// A declined rule must be in the inventory, must not be registered, and must name a reason.
//
// Each of those can rot in a different direction and none of them is visible from the decline entry
// itself. An entry for a rule the inventory does not carry is a decline of nothing, and it silently
// shrinks the denominator the project is judged on. An entry for a rule somebody later ported reads
// as a standing decision while the code says otherwise, and the log line would keep asserting a
// choice nobody is making any more.
//
// The reason is required for the same argument the exemption map makes: an entry with no reason says
// only that somebody wanted a number to move.
func TestEveryDeclinedRuleIsRealAndUnported(t *testing.T) {
	inventory := readInventory(t)
	registered := registeredRuleNames()

	inInventory := make(map[string]bool, len(inventory))
	for _, entry := range inventory {
		inInventory[entry] = true
	}

	for entry, reason := range rulesDeclined {
		if !inInventory[entry] {
			t.Errorf("rule %q is declined but is not in the inventory, so it declines nothing and "+
				"quietly lowers the denominator", entry)
		}
		if name, found := matchRegistered(entry, registered); found {
			t.Errorf("rule %q is declined and also registered as %q, so the decline is stale and "+
				"the log asserts a decision nobody is making", entry, name)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("rule %q is declined with no reason, which records that somebody wanted a "+
				"number to move rather than that anybody looked", entry)
		}
	}
}
