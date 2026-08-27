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
// every rule in the catalog registers bare, and `configuration.settingFor` performs the same split on the
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
// TestRegisteredNamesMatchTheirUpstreamSpelling replaces an earlier guard that required the
// opposite.
//
// Rules used to register bare, and `bareRuleName` stripped any prefix before matching, so a config
// key spelled `bogusplugin/no-alert` resolved exactly like the correct one -- measured, same six
// findings, no warning. ESLint is not that forgiving: an unknown rule key is fatal there, so a
// wrong prefix killed the whole run twice in one day while `s l --linter both` kept printing a
// comparison against a linter that had linted nothing.
//
// Registering the real upstream name removes the translation step. One name is right everywhere,
// and a wrong prefix now fails here rather than in ESLint.
//
// A plugin rule must carry its plugin's namespace; an ESLint core rule must carry none, because
// upstream has none. Anything else is the inconsistency the old guard was really protecting.
func TestRegisteredNamesMatchTheirUpstreamSpelling(t *testing.T) {
	registered := registeredRuleNames()
	if len(registered) == 0 {
		t.Fatal("registry.All() returned no rules, so this test checked nothing")
	}

	// The namespaces a rule may carry. A rule with no slash is an ESLint core rule or one of this
	// tree's own, both of which are correct bare.
	knownNamespaces := map[string]bool{
		"@typescript-eslint": true,
		// `base` is api-phi-health's own lint layer, the same kind of namespace as structure and
		// nexus below: rules this organization wrote rather than ported from a plugin. It is not an
		// ESLint plugin prefix, so nothing translates it on the way out.
		"base":               true,
		"react":              true,
		"react-hooks":        true,
		"@next/next":         true,
		"better-tailwindcss": true,
		"structure":          true,
		"nexus":              true,
	}

	for _, name := range registered {
		slash := strings.LastIndex(name, "/")
		if slash < 0 {
			continue
		}
		namespace := name[:slash]
		if knownNamespaces[namespace] {
			continue
		}
		t.Errorf("rule %q carries the namespace %q, which is not one this tree recognizes; a rule "+
			"registers under its real upstream name, and an unrecognized prefix is the spelling "+
			"that reaches ESLint and kills the run", name, namespace)
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
// split `configuration.settingFor` resolves. The suffix must fall on a `/` boundary for the same reason it
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
	// Ported from eslint-plugin-react and enabled in both engines by this port. Upstream marks it
	// `recommended: false`, so it is off in the plugin's own recommended config, and there is no
	// oxlint config in the tree to have carried it either. A rule the gate never had rather than a
	// parity gap.
	//
	// It is a judgment about React prop type declarations, so it was enabled on the frontend layer.
	// The audit measured zero violations, which makes it a guardrail against drift.
	//
	// One arm is deliberately not reproduced and the reason is a missing substrate rather than a
	// decision. Upstream also looks inside a prop wrapper call, and which functions count comes
	// from `settings.propWrapperFunctions`, a setting shared by four rules in this plugin that
	// `rule.Context` has no path to. Fourteen of upstream's 63 reporting corpus cases configure it
	// and are not imported; the other 97 cases, including all 48 passing ones, are.
	"react/forbid-prop-types": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and the audit measured zero violations in ahra; its prop-wrapper arm is declined for want of a settings surface, see the comment above",

	// Ported from eslint-plugin-react and enabled in both engines by this port. Upstream marks it
	// `recommended: false`, so it is off in the plugin's own recommended config, and there is no
	// oxlint config in the tree to have carried it either. A rule the gate never had rather than a
	// parity gap.
	//
	// It is a judgment about React keys, so it was enabled on the frontend layer. The audit
	// measured eighty-four violations, the largest of this batch, so enabling it is a real cleanup
	// rather than a guardrail. Each is a list whose keys are array positions, which React reuses
	// across a reorder and which therefore carries element state onto the wrong item.
	"react/no-array-index-key": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and the audit measured eighty-four violations in ahra",

	// Ported from eslint-plugin-react and enabled in both engines by this port. Upstream marks it
	// `recommended: false`, so it is off in the plugin's own recommended config, and there is no
	// oxlint config in the tree to have carried it either. A rule the gate never had rather than a
	// parity gap.
	//
	// It is a judgment about React context providers, so it was enabled on the frontend layer.
	// Unlike the three react entries below it, the audit measured eighteen violations rather than
	// zero, so enabling it is a cleanup rather than only a guardrail. Each is a value handed to a
	// provider that is rebuilt every render, which defeats the identity comparison every consumer
	// of that context relies on.
	"react/jsx-no-constructed-context-values": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and the audit measured eighteen violations in ahra",

	// Ported from eslint-plugin-react and enabled in both engines by this port. Upstream marks it
	// `recommended: false`, the same shape as the state-in-constructor entry below: it is off in
	// the plugin's own recommended config, and there is no oxlint config in the tree to have
	// carried it either. A rule the gate never had rather than a parity gap.
	//
	// It is a judgment about how a React component is declared, so it was enabled on the frontend
	// layer. The audit measured zero violations in ahra, which is expected rather than surprising:
	// the factory it reports on was removed from React itself and now ships as a separate package
	// nothing in this tree depends on. A guardrail against drift.
	"react/prefer-es6-class": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and the audit measured zero violations in ahra",

	// Ported from eslint-plugin-react and enabled in both engines by this port. Unlike the
	// state-in-constructor entry below, upstream marks THIS one `recommended: true`, so its absence
	// from the inventory says something about the configuration being replaced rather than about
	// the rule: neither tool named it when the inventory was captured. A rule the gate never had
	// rather than a parity gap.
	//
	// It is a judgment about JSX children, so it was enabled on the frontend layer. The audit
	// measured zero violations in ahra, which makes it a guardrail against drift. That zero is
	// worth more here than on a stylistic rule: the failure this catches is silent, since a comment
	// left outside braces renders to the page and nothing warns.
	"react/jsx-no-comment-textnodes": "ported from eslint-plugin-react, which recommends it; not enforced by either tool when the inventory was captured, and the audit measured zero violations in ahra",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory records
	// what the two tools enforced when it was captured, and neither enforced this one: upstream marks
	// it `recommended: false`, so it is off in the plugin's own recommended config and nothing turned
	// it on here, which is why it is outside the inventory rather than a parity gap.
	//
	// It is a judgment about JSX elements, so it was enabled on the frontend layer. The audit measured
	// zero violations in ahra and a dry run over the tree reproduced that zero, which makes it a
	// guardrail against drift rather than a cleanup. The zero is worth more here than on a stylistic
	// rule, because what it guards is a security boundary: an unsandboxed iframe hands a third-party
	// document the privileges of a top-level page, and nothing about the code looks wrong when it
	// happens.
	"react/iframe-missing-sandbox": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and the audit measured zero violations in ahra, which a dry run over the tree reproduced",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory records
	// what the two tools enforced when it was captured, and neither enforced this one: upstream marks
	// it `recommended: false`, so it is off in the plugin's own recommended config and nothing turned
	// it on here, which is why it is outside the inventory rather than a parity gap.
	//
	// It is a judgment about React component declarations, so it was enabled on the frontend layer.
	// The audit measured zero violations in ahra and a dry run over the tree reproduced that zero,
	// which makes it a guardrail against drift. What it guards is a failure that only appears in the
	// production build: React strips propTypes off components there, so an expression reading another
	// component's propTypes is an object in development and undefined in the build users run.
	"react/forbid-foreign-prop-types": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and the audit measured zero violations in ahra, which a dry run over the tree reproduced",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory records
	// what the two tools enforced when it was captured, and neither enforced this one: upstream marks
	// it `recommended: false`, so it is off in the plugin's own recommended config and nothing turned
	// it on here, which is why it is outside the inventory rather than a parity gap.
	//
	// It is a judgment about JSX elements, so it was enabled on the frontend layer. This rule is
	// unusual in that it enforces NOTHING until somebody writes a forbid list: the whole judgment is
	// supplied by the config, and an unconfigured rule declines every element. So it is registered and
	// enabled as scaffolding for a decision this project has not made yet, and the dry run's zero over
	// the tree is what an empty list produces rather than evidence about the code.
	"react/forbid-elements": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and it enforces nothing until a forbid list is configured, so the audit's zero and the dry run's zero both reflect an empty list",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory
	// records what the two tools enforced when it was captured, and neither enforced this one:
	// upstream marks it `recommended: false`.
	//
	// A dry run over the tree found zero findings across 3,516 files against 95,778 registrations,
	// with no crashes. A seeded probe tree reported two findings on five candidate class fields,
	// declining a non-lifecycle name, a static field checked against the other list, and a class
	// with no React base. The fixer was then run against that tree and produced source that lints
	// clean and parses, converting a block body to a method and a parenthesized object concise body
	// to a returning block with the parentheses removed.
	//
	// This port declines the repair on any parameter that is not a plain identifier, which upstream
	// ships. Upstream builds its parameter list by mapping each parameter to its identifier name,
	// undefined for a destructured, defaulted or typed parameter, so it writes the literal text
	// `undefined` into the repaired source and silently deletes what the parameter said. A fix is
	// applied unattended, so reporting without one is the subset that can be shown correct. The
	// typed-parameter case is the one this tree actually contains and upstream's corpus cannot.
	"react/no-arrow-function-lifecycle": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and a dry run over the tree measured zero findings against 95,778 registrations, with a seeded probe tree confirming both the rule and its repair",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory
	// records what the two tools enforced when it was captured, and neither enforced this one:
	// upstream marks it `recommended: false`.
	//
	// A dry run over the tree found zero findings across 3,516 files against 117,763 registrations,
	// and the non-zero registration count is what separates a clean tree from an inert rule. A
	// seeded probe tree reported five findings on eight candidate style props, declining the
	// object, the null and the undefined.
	//
	// Two things this port had to establish that upstream's corpus does not state. Upstream reads
	// the FIRST declaration of a name, so a variable declared twice is judged by the one written
	// first, and a loop over declarations would be wider than the rule. And a shorthand property
	// resolves through `GetShorthandAssignmentValueSymbol` rather than the plain accessor, which
	// answers the property's own symbol and made the rule silent on an input upstream reports.
	"react/style-prop-object": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and a dry run over the tree measured zero findings against 117,763 registrations, with a seeded probe tree confirming the rule fires on five of eight candidate style props",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory
	// records what the two tools enforced when it was captured, and neither enforced this one:
	// upstream marks it `recommended: false`.
	//
	// Like `forbid-elements`, this rule enforces nothing until a forbid list is configured, so the
	// audit's zero and the dry run's zero both reflect an empty list rather than a clean tree. A
	// seeded probe tree with a configured list reported three findings on five candidate
	// attributes, declining a component tag, an unlisted prop and a tag whose first character has
	// no case.
	//
	// The clone and the INSTALLED build disagree here and this port follows the installed build.
	// The clone's working tree carries a `disallowedValues` feature and a second message id that
	// the installed 7.37.5 does not have; replaying upstream's own corpus against the installed
	// build disagrees on exactly the three cases exercising it. The installed build is the artifact
	// our gate compares against, so a `disallowedValues` key is accepted by the schema and ignored,
	// exactly as the installed build ignores it.
	"react/forbid-dom-props": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and it enforces nothing until a forbid list is configured, so the audit's zero and the dry run's zero both reflect an empty list",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory
	// records what the two tools enforced when it was captured, and neither enforced this one:
	// upstream marks it `recommended: false`, so it is off in the plugin's own recommended config
	// and nothing turned it on here.
	//
	// It is a judgment about JSX attributes, so it was enabled on the frontend layer. A dry run
	// over the tree found zero findings across 3,516 files with 22,260 registrations, and the
	// non-zero registration count is what separates a clean tree from an inert rule. A seeded
	// probe tree holding two `javascript:` anchors and one ordinary one reported exactly the two,
	// on the attribute rather than the element.
	//
	// One half of upstream's option surface has no substrate here and it costs findings. Upstream
	// also reads `settings.linkComponents` from ESLint's shared settings; verify has no
	// shared-settings surface, so this port answers as though they were empty. Measured on
	// upstream's own corpus, that turns two reporting cases silent and drops a third from two
	// findings to one. Everything configured through the rule's own options is exact.
	"react/jsx-no-script-url": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and a dry run over the tree measured zero findings against 22,260 registrations, with a seeded probe tree confirming the rule fires",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory records
	// what the two tools enforced when it was captured, and neither enforced this one: upstream marks
	// it `recommended: false`, so it is off in the plugin's own recommended config and nothing turned
	// it on here, which is why it is outside the inventory rather than a parity gap.
	//
	// It is a judgment about JSX children, so it was enabled on the frontend layer. The audit measured
	// zero violations in ahra and a dry run over the tree reproduced that zero.
	//
	// This port diverges from upstream in one place and the divergence is a refusal to crash. Upstream
	// indexes the first argument of any call among the children without checking there is one, so an
	// ordinary call in that position takes the linter down, measured on the installed build. Here that
	// is guarded, reaching the verdict upstream would have reached had it not thrown.
	"react/no-adjacent-inline-elements": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and the audit measured zero violations in ahra, which a dry run over the tree reproduced",

	// Ported from eslint-plugin-react and enabled in both engines by this port. The inventory
	// records what the two tools enforced when it was captured, and neither enforced this one:
	// upstream marks it `recommended: false`, so it is off in the plugin's own recommended config,
	// and there is no oxlint config in the tree to have carried it either. A rule the gate never
	// had rather than a parity gap.
	//
	// It is a judgment about React class components, so it was enabled on the frontend layer. The
	// audit measured zero violations in ahra, which makes it a guardrail against drift rather than
	// a cleanup: nothing in the tree writes a class component with a `state` class property today.
	"react/state-in-constructor": "ported from eslint-plugin-react, which marks it recommended:false; not enforced by either tool when the inventory was captured, and the audit measured zero violations in ahra",

	// Registered in the nexus plugin map at NexusLintConfiguration.ts:31 and, until it was turned on
	// in ahra, enabled in no rules block anywhere: the plugin knew the name and nothing turned it on.
	// It is enabled now, in both engines, and it stays outside the inventory for a different reason
	// than it started with. The inventory records what the two tools being replaced enforced at the
	// moment it was captured, and at that moment this rule enforced nothing. A rule enabled after
	// the capture is not a parity gap; it is a rule the gate never had.
	"nexus/import-require-path-alias": "not enforced by either tool when the inventory was captured, and enabled after it",

	// Ported from typescript-eslint and enabled in both engines by this port, the same shape as the
	// no-redeclare entry below. The inventory records what the two tools enforced when it was
	// captured, and neither enforced this one: it is in typescript-eslint's `strict` preset rather
	// than its `recommended` one, so a project on the recommended set never had it, and there is no
	// oxlint config in the tree to have carried it either. A rule the gate never had rather than a
	// parity gap.
	"@typescript-eslint/use-unknown-in-catch-callback-variable": "ported from typescript-eslint, whose strict preset carries it; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint and enabled in both engines by this port, the same shape as the
	// entries around it. Unlike the one above, this rule IS in upstream's recommended preset, so its
	// absence from the inventory says something about the configuration being replaced rather than
	// about the rule: neither tool named it when the inventory was captured. A rule the gate never
	// had rather than a parity gap.
	"@typescript-eslint/restrict-plus-operands": "ported from typescript-eslint, which recommends it; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint and enabled in both engines by this port, the same shape as the
	// entries around it: it lives in upstream's `strict` preset rather than its `recommended` one, so
	// a project on the recommended set never had it, and there is no oxlint config in the tree to
	// have carried it either. A rule the gate never had rather than a parity gap.
	//
	// It is a judgment about any TypeScript object rather than about the DOM, so it was enabled on
	// the universal layer. The audit measured sixteen sites in ahra and none of them is auto-fixable,
	// so turning this on is a cleanup somebody has to do by hand rather than a command.
	"@typescript-eslint/no-dynamic-delete": "ported from typescript-eslint, whose strict preset carries it; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint and enabled in both engines by this port, the same shape as the
	// entries around it. Upstream ships it in no preset at all, recommended or strict, so neither
	// tool being replaced could have carried it and its absence from the inventory says nothing
	// about the rule. A rule the gate never had rather than a parity gap.
	//
	// The repair is three suggestions rather than a fix, deliberately: the correct enum value is not
	// recoverable from the source, so nothing may rewrite one unattended.
	"@typescript-eslint/prefer-enum-initializers": "ported from typescript-eslint, which ships it in no preset; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint and enabled in both engines by this port. Upstream carries it in
	// its STRICT preset rather than its recommended one, so neither tool being replaced had it on and
	// its absence from the inventory says nothing about the rule. A rule the gate never had rather
	// than a parity gap.
	//
	// The audit measured two violations in ahra, both in one test file, and both are the shape the
	// rule exists to catch: an enum member computed from another member outside a bitwise expression.
	// It ships no fixer, upstream or here, because the correct literal is not recoverable from the
	// source.
	"@typescript-eslint/prefer-literal-enum-member": "ported from typescript-eslint, whose strict preset carries it; not enforced by either tool when the inventory was captured, and the audit measured 2 violations in one test file",

	// Ported from typescript-eslint and enabled in both engines by this port. Upstream carries it in
	// its STYLISTIC preset, which neither tool being replaced had on, so its absence from the
	// inventory says nothing about the rule. A rule the gate never had rather than a parity gap.
	//
	// The audit measured 55 violations in ahra and this port measures 57, spread over about thirty
	// files with no single-file cluster of the kind a false-positive class produces. Every site the
	// audit named by path and line is among them at the same line and column. The two-finding gap is
	// not explained here: the audit was captured earlier and the tree has moved, and nothing was found
	// to suggest the difference is the rule.
	//
	// They are auto-fixable, and the fixer is upstream's own repair reproduced against its nine
	// recorded outputs, so the cleanup is a fix run plus a read of the diff rather than 57 hand edits.
	// A count that size is a decision for Kirk rather than for a porter, and it is named here as well
	// as in the port's own commit so it cannot land quietly.
	"@typescript-eslint/non-nullable-type-assertion-style": "ported from typescript-eslint, whose stylistic preset carries it; not enforced by either tool when the inventory was captured, and this port measures 57 violations against the audit's 55, all auto-fixable",

	// Ported from typescript-eslint and enabled in both engines by this port. Upstream carries it in
	// its STRICT preset, which neither tool being replaced had on, so its absence from the inventory
	// says nothing about the rule. A rule the gate never had rather than a parity gap.
	//
	// The audit measured 15 violations in ahra and this port measures the same count. They are not
	// auto-fixable and each is a real decision, since turning a static-only class into a module is a
	// change to how every caller imports it. Most are the same shape, a module-style api object
	// written as a class of statics.
	"@typescript-eslint/no-extraneous-class": "ported from typescript-eslint, whose strict preset carries it; not enforced by either tool when the inventory was captured, and the audit measured 15 violations, none auto-fixable",

	// Ported from typescript-eslint and enabled in both engines by this port. Upstream carries it in
	// its STYLISTIC preset, which neither tool being replaced had on, so its absence from the
	// inventory says nothing about the rule. A rule the gate never had rather than a parity gap.
	//
	// The audit measured 5 violations in ahra and this port measures the same count. They are not
	// auto-fixable, upstream ships no fixer either, and each is a small mechanical rewrite of a
	// counted loop into a for-of.
	"@typescript-eslint/prefer-for-of": "ported from typescript-eslint, whose stylistic preset carries it; not enforced by either tool when the inventory was captured, and the audit measured 5 violations, none auto-fixable",

	// Ported from typescript-eslint and enabled in both engines by this port. Upstream marks it frozen
	// and ships it in no preset, so neither tool being replaced had it on and its absence from the
	// inventory says nothing about the rule. A rule the gate never had rather than a parity gap.
	//
	// The audit measured zero violations and this port measures zero, and the reason is NOT that the
	// tree satisfies the rule. Upstream declares `defaultOptions: ['always']` as a createRule property
	// rather than as `meta.defaultOptions`, and ESLint 10 reads only the latter, so a rule named as a
	// bare severity string is handed no mode and does nothing at all. Measured on the installed build,
	// and EnableRule.ts writes exactly that spelling.
	//
	// So the rule is registered, enabled, and deliberately inert, matching the gate being replaced. A
	// port defaulting to always instead would put 512 findings on this tree, which is what the first
	// version of this one did before the dry run disagreed with the audit. Giving it a mode is a
	// one-line config change and a 512-finding decision, and it belongs to Kirk rather than a porter.
	//
	// A seeded tree configured with an explicit mode reported on every seeded shape and left the
	// initialized ones alone, so the rule can see; only the configuration is silent.
	"@typescript-eslint/init-declarations": "ported from typescript-eslint, which ships it frozen and in no preset; not enforced by either tool when the inventory was captured, and it is inert under a bare severity because upstream's default mode never reaches the rule, so both the audit and this port measure zero",

	// Ported from typescript-eslint and registered, but deliberately NOT enabled: the live config
	// already turns it off at VerifySettings.json:370 under the `typescript/` spelling, and the port
	// does not reverse somebody's standing decision. The reason is recorded in full beside its entry
	// in the live-wiring guard's own exemption map, which is where a reader looking at why it runs on
	// nothing will land.
	//
	// It is outside the inventory for the ordinary reason on top of that: neither tool being replaced
	// enforced it when the capture was taken.
	"@typescript-eslint/require-array-sort-compare": "ported and registered but left off, matching an explicit off in the live config; not enforced by either tool when the inventory was captured",

	// Ported and registered, deliberately not enabled, for the same reason as the entry above and
	// against the same block of hand-maintained disables. The reason is recorded in full beside its
	// entry in the live-wiring guard's exemption map.
	"@typescript-eslint/no-meaningless-void-operator": "ported and registered but left off, matching an explicit off in the live config; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint and enabled in both engines by this port. Unlike the two entries
	// above it carries no prior decision in the live config to override, so enabling it is the
	// ordinary path rather than a judgment: the audit measures zero violations in the tree, which
	// makes it a guardrail against drift rather than a cleanup.
	//
	// Outside the inventory for the usual reason. It sits in upstream's `strict` preset rather than
	// its `recommended` one, so a project on the recommended set never had it, and neither tool
	// being replaced enforced it when the capture was taken.
	"@typescript-eslint/no-non-null-asserted-nullish-coalescing": "ported from typescript-eslint, whose strict preset carries it; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint, whose strict preset carries it, and enabled in both engines by
	// this port. It is outside the inventory for the usual reason, that neither tool enforced it when
	// the capture was taken, but this one has a wrinkle worth recording because the next reader will
	// meet it.
	//
	// The live verify config names this rule TWICE, once as `typescript/no-misused-spread` set to
	// "off" in a hand-maintained list of deliberately disabled rules, and once as the bare
	// `no-misused-spread` set to "error" that enabling it added. They are different key strings, so
	// this is not a duplicate key and nothing silently wins: `settingFor` tries the exact registry
	// name first and only then falls back to a prefixed one, so the bare "error" resolves and the
	// rule runs. Measured on a seeded tree carrying both keys, which reported.
	//
	// The stale "off" is therefore inert rather than harmful, and it is left alone rather than
	// removed, because it is somebody's recorded decision about the prefixed spelling and reversing
	// it is not this port's call. The eslint side carries no such contradiction.
	"@typescript-eslint/no-misused-spread": "ported from typescript-eslint, whose strict preset carries it; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint and enabled in both engines by this port, so like the entry above
	// it is a rule the gate never had rather than a parity gap. The inventory records what the two
	// tools enforced when it was captured and neither enforced this one: the eslint side had no
	// `@typescript-eslint/no-redeclare` line until this port added it, and there is no oxlint config
	// in the tree to have carried it either.
	//
	// Worth naming which rule this is, because two different ones share the spelling and they
	// disagree. Core `no-redeclare` reports all five of TypeScript's legitimate declaration merges,
	// measured against the installed build: two interfaces, a class beside an interface, a class
	// beside a namespace, an enum beside a namespace, and a pair of overload signatures. The
	// typescript-eslint variant is silent on every one of those and is what is ported here, so the
	// bare registered name means the typescript variant rather than the core rule it is spelled like.
	//
	// One of upstream's two options is deliberately not ported, and the decline is recorded here
	// rather than only in the rule's doc comment because this is where a coverage audit looks.
	// `builtinGlobals` reports a declaration that shadows a global. It is not a gap in our checker:
	// probed with a control, a local `var Object` SHADOWS rather than merges, so the scope
	// enumeration returns only the local declaration and the standard library's `Object` is not in
	// scope at all, answering identically to a name that is not a builtin. There is nothing for the
	// option to compare against.
	//
	// The verdict is eslint's environment model rather than name resolution, and upstream's own
	// corpus is what proves it: `var Object = 0;` appears as a CLEAN case and as a REPORTING case
	// under the SAME `builtinGlobals: true`, separated only by whether the file is a module. Sixteen
	// of upstream's 52 cases carry the option and are omitted from the fixtures, pinned by
	// `TestNoRedeclareBuiltinGlobalsIsOutOfScope` rather than left as silence. The same reasoning
	// covers upstream's `/*global b:false*/` case, which needs a directive-globals surface we also
	// do not have. Reinstating either means building a configured-globals surface first, not
	// changing this rule.
	"@typescript-eslint/no-redeclare": "ported from typescript-eslint and enabled by that port; not enforced by either " +
		"tool when the inventory was captured; upstream's builtinGlobals option is declined for want " +
		"of a configured-globals surface, see the comment above",

	// Ported from typescript-eslint, whose stylistic preset carries it, and enabled in both engines
	// by this port. Outside the inventory for the usual reason, and this one is worth stating
	// carefully because it LOOKS like a parity gap and is not: the rule audit measured 711
	// violations in the ahra tree, so the rule finds real work. Those 711 came from running the rule
	// explicitly, not from either tool enforcing it, and the inventory records what was enforced
	// when it was captured. A rule that finds work is not the same as a rule the gate had.
	//
	// So enabling this one is a cleanup rather than a guardrail, unlike the two entries below it.
	// The rule is fully fixable and the repair was checked against upstream's rather than eyeballed:
	// all ninety-nine of upstream's own before-and-after pairs reproduce byte for byte, and on a
	// file holding six real shapes from this tree the two fixers wrote identical output.
	//
	// One defect worth recording because no fixture could see it. A heritage clause is its own node
	// type in TSESTree, so upstream's type-reference visitor never fires on `interface I extends
	// Array<string> {}`; typescript-go reuses `KindTypeReference` there, so the first draft reported
	// it and would have proposed a rewrite that does not parse. Found by diffing the two linters
	// over the real tree, 713 against 711, both extra in one file. After the guard, the two agree
	// exactly: 711 findings over 190 files with nothing on either side of the diff.
	// Ported from typescript-eslint, whose stylistic preset carries it, and enabled in both engines
	// by this port. Outside the inventory for the usual reason: neither tool named it at capture.
	//
	// The rule is two rules in one file and only one of them has exposure here. Measured on this
	// tree with a control: five `indexOf` presence comparisons against 260 regex `test` sites, and
	// the dry run splits its thirty findings five to twenty five the same way. So the regex half
	// carries the value, and it is also the half with no substrate on the shelf.
	//
	// That absence is worth recording because the obvious build is wrong. `regexpattern.Walk` looks
	// like the right foundation and cannot answer this rule's question: run over these patterns it
	// renders an unescaped dot as a dot, renders an anchored pattern and a grouped one and a
	// non-capturing group all as the bare text, and renders an alternation as one run of
	// characters. A port built on it would rewrite an anchored pattern into a substring test, which
	// is wrong, and upstream's corpus writes only three of those shapes so no imported fixture
	// would catch it. The shelf's own doc says a caller wanting alternation should reach for the
	// layer underneath, and that is what this does.
	//
	// Two narrowings are recorded at the line rather than left silent, both costing findings rather
	// than adding them. A sentinel written as a named constant is evaluated upstream and declined
	// here, and this tree has none. A regex receiver is resolved only through a file-scope variable.
	// Ported from typescript-eslint and enabled in both engines by this port. Outside the inventory
	// for the usual reason: neither tool named it when the capture was taken. Upstream ships it in
	// no preset at all, which is why it was never enforced anywhere.
	//
	// The repair rebuilds a signature, which is the shape that lost type information twice in this
	// project. It is safe here for the same reason `class-literal-property-style` is: the parts that
	// can hold anything are COPIED as raw source rather than re-rendered. The parameter list is
	// spliced from its opening parenthesis to its closing one and the type parameter list is copied
	// whole, so a generic constraint, an optional parameter, a rest parameter and a comment inside
	// the parentheses all survive without being enumerated. Eleven rows pin that, and each rewrite
	// was additionally checked through the compiler to confirm the member's type is unchanged.
	//
	// Upstream declines in three places and all three are reproduced: a `this` return type has no
	// property spelling, a member inside a module declaration is reported without a repair, and a
	// `readonly` function property is offered as a SUGGESTION because converting it drops the
	// modifier and the member becomes reassignable. That last one is the same judgment this project
	// applies when it withholds a fixer that would change meaning, arrived at independently.
	//
	// One divergence in fix SHAPE with no divergence in judgment. Upstream attaches the whole-group
	// rewrite for a set of overloads to every signature in it, producing overlapping fixes that
	// ESLint resolves by applying one per pass and re-linting. Our engine flattens fixes into
	// independent proposals and refuses an overlap, so the repair is attached to the first signature
	// only and written as the single merged replacement ESLint actually performs. Measured: the two
	// write identical source.
	//
	// The audit recorded 39 violations and this rule finds 49. That is the audit's number being
	// stale rather than a divergence: driven over the same twenty-five files, this rule and the
	// installed build produce byte-identical finding sets, 49 against 49 with nothing on either side
	// of the diff, including the one the audit quotes as its example.
	// Ported from typescript-eslint, whose strict preset carries it, and enabled in both engines by
	// this port. Outside the inventory for the usual reason: neither tool named it at capture.
	//
	// The repair is safe by construction rather than by care, which is worth recording because this
	// is the shape that lost type information twice in this project. The replacement is built from
	// the compared expression's OWN SOURCE TEXT plus punctuation, so nothing inside it is
	// re-rendered and nothing inside it can be lost; the span being replaced holds only the
	// comparison operator and the boolean literal. Seven rows pin that, including a generic call, a
	// type assertion, a satisfies expression, a non-null assertion and a comment.
	//
	// Two things about our parser that upstream cannot meet. It keeps parentheses where TSESTree
	// deletes them, and three separate sites needed the unwrap: the literal side, the walk out to a
	// wrapping negation, and the compared expression itself. Ten of upstream's own reporting cases
	// failed before all three were handled. And the rule's `noStrictNullCheck` arm is unreachable
	// through this project's harness, which writes its own tsconfig after the setup hook runs, so it
	// is pinned by a unit test on the option resolution rather than by a rule fixture.
	//
	// The audit recorded 33 violations and this rule finds 35. That is the audit being stale rather
	// than a divergence: driven over the same twenty files, this rule and the installed build
	// produce byte-identical finding sets with nothing on either side of the diff.
	// Ported from typescript-eslint and enabled in both engines by this port. Outside the inventory
	// for the usual reason: neither tool named it when the capture was taken.
	//
	// Half this rule's judgment is about a DIFFERENT file, which is why it declares that it reads
	// the program: an exported name is type-only or not according to what the module it came from
	// declares, and the star arm reads another module's exports outright. The star arm reproduces an
	// upstream workaround rather than improving on it, because the thing it works around is the same
	// here: a name reaching a module through a type-only star sits in a table the checker does not
	// expose, and the only way to see it is that one lookup lists the name while another does not.
	// Probed on a type-only module and a value-bearing one, the pair discriminates.
	//
	// The repair SPLITS a statement, which was flagged as the riskiest shape in this batch. It is
	// safe because a specifier holds only names, so re-rendering one can lose nothing a copy would
	// have kept; what it can lose is SPELLING, and that is pinned by rows covering an alias, a
	// single-quoted export name, a double-quoted one and an emoji literal, all carried through as
	// raw source rather than cooked values.
	//
	// Two defects the imported corpus caught. The star fixer scanned bytes for its asterisk and
	// found the one inside a block comment, writing the keyword into the comment; upstream's own
	// corpus is the only place that shape appears. And the rendered specifier order is BUCKET order
	// rather than source order, so a statement mixing an inline type with a checker-found one comes
	// back reversed, which is upstream's behavior and reads as a bug until measured.
	//
	// Zero findings on this tree, matching the audit. Separated from an inert rule with a seeded
	// probe: two findings on two seeded exports, silent on an already-inline one and on an
	// all-values one, and the repair byte-identical to the installed build on both.
	"@typescript-eslint/consistent-type-exports": "ported from typescript-eslint; not enforced by " +
		"either tool when the inventory was captured; its export-star arm reproduces upstream's " +
		"two-lookup workaround for type-only star re-exports, which the checker does not expose " +
		"directly",

	"@typescript-eslint/no-unnecessary-boolean-literal-compare": "ported from typescript-eslint, " +
		"whose strict preset carries it; not enforced by either tool when the inventory was " +
		"captured; its strictNullChecks arm is unreachable through the test harness and is pinned " +
		"by a unit test on the option resolution instead",

	"@typescript-eslint/method-signature-style": "ported from typescript-eslint, which ships it in " +
		"no preset; not enforced by either tool when the inventory was captured; its overload repair " +
		"is written as one merged replacement because this engine refuses the overlapping pair " +
		"upstream emits",

	"@typescript-eslint/prefer-includes": "ported from typescript-eslint, whose stylistic preset " +
		"carries it; not enforced by either tool when the inventory was captured; its regex half " +
		"reads patterns through a reader written on regexsyntax because the shelf walker cannot " +
		"distinguish an anchor or an alternation from a literal",

	// Ported from typescript-eslint, whose stylistic preset carries it, and enabled in both engines
	// by this port. Outside the inventory for the usual reason: neither of the two tools the
	// inventory records named this rule when the capture was taken.
	//
	// It offers SUGGESTIONS rather than fixes, which is the whole safety story and is worth
	// recording here because a coverage audit would otherwise read the missing fixer as an
	// unfinished port. Upstream's `meta` carries `hasSuggestions` and no `fixable`, measured by
	// running the installed build: `verifyAndFix` on a reporting input returns unchanged source.
	// That matters, because the getters direction genuinely widens types. Measured through the
	// TypeScript compiler on the rewritten source, `readonly x: 1 | 2 = 1` becomes a getter of type
	// `number`, and so does the unannotated `readonly x = 1` that is upstream's own core case. The
	// widening is upstream's deliberate semantics and is reproduced; it is only defensible because
	// a human chooses it.
	//
	// The field direction preserves types rather than declining, and the mechanism is worth copying
	// into the next fixable port. It splices the raw source between the parameter list's closing
	// parenthesis and the body's opening brace instead of re-rendering a signature, so a return
	// annotation, its spacing and any comment written there all survive byte for byte without being
	// enumerated. On the real tree every one of its eight findings is an annotated getter, and the
	// suggestion keeps the annotation.
	"@typescript-eslint/class-literal-property-style": "ported from typescript-eslint, whose " +
		"stylistic preset carries it; not enforced by either tool when the inventory was captured; " +
		"offers suggestions rather than fixes, matching upstream",

	"@typescript-eslint/array-type": "ported from typescript-eslint, whose stylistic preset carries " +
		"it; not enforced by either tool when the inventory was captured, though the audit measured " +
		"711 violations it will now clean up",

	// Ported from typescript-eslint, whose stylistic preset carries it, and enabled in both engines
	// by this port. Outside the inventory for the usual reason: neither of the two tools the
	// inventory records named this rule when the capture was taken. Like the ban-tslint-comment
	// entry below it, the ahra tree already satisfies it, so enabling it is a guardrail against
	// drift rather than a cleanup.
	//
	// No divergence in judgment, and two parser differences worth naming because both would read as
	// divergences to someone diffing the two implementations. TSESTree wraps an exported declaration
	// in an export node and typescript-go carries `export` as a modifier, so upstream's unwrapping
	// recursion has nothing to unwrap here; the reported span is identical either way, measured.
	// And TSESTree folds a method, a getter, a setter and a constructor into one node type while
	// giving an ABSTRACT method a separate type that upstream's switch does not name, so an
	// abstract member is silently not a method to this rule. That exclusion has to be written out
	// against our parser rather than inherited, and it was measured with a concrete control so that
	// silence could be told apart from a shape the rule never reached.
	"@typescript-eslint/adjacent-overload-signatures": "ported from typescript-eslint, whose " +
		"stylistic preset carries it; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint, whose stylistic preset carries it, and enabled in both engines
	// by this port. Outside the inventory for the usual reason: the inventory records what the two
	// tools enforced when it was captured, and neither named this rule. It is in upstream's
	// `stylistic` preset rather than its `recommended` one, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// Worth recording that the ahra tree has zero violations of it today, measured by the rule audit
	// and reproduced by this port's dry run, which offered the rule 3,503 files and found nothing in
	// any of them. So enabling it buys no cleanup and is a guardrail against drift, which is a
	// different kind of value than a rule that finds work.
	//
	// One divergence, recorded here as well as at the line because this is where a coverage audit
	// looks. Upstream's fixer widens its removal by one character on each side without testing what
	// those characters are, so measured on the installed build it rewrites `x;// tslint:disable` to
	// `x` and `/* tslint:disable */let x = 1;` to `et x = 1;`. The second is refused by this
	// project's parse guard and the first is not, and a fix is applied unattended. This port widens
	// only onto whitespace, which reproduces all eight of upstream's own corpus outputs byte for
	// byte while declining to delete a character that is not blank. The judgment is identical: the
	// same inputs report, at the same span, with the same rendered text.
	"@typescript-eslint/ban-tslint-comment": "ported from typescript-eslint, whose stylistic preset " +
		"carries it; not enforced by either tool when the inventory was captured; upstream's " +
		"one-character-either-side fixer is narrowed to widen only onto whitespace, see the comment above",

	// A house rule with no upstream on either side, written from reasoning rather than ported, so
	// no inventory entry could exist for it. It guards verify's own type-based React rules rather
	// than the source: in a file where a hook call resolves to `any`, `set-state-in-render` and
	// `set-state-in-effect` report nothing and nothing says a check was skipped. The gate being
	// replaced has no equivalent because it has no such rules to protect.
	"structure/react-hook-any-type": "a house tripwire over verify's own type-based React rules; the gate " +
		"being replaced has no rule it could correspond to",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either. A rule the
	// gate never had rather than a parity gap.
	//
	// This one needs stating carefully because it is a CLEANUP rather than a guardrail, and the
	// audit rated it Maybe rather than Yes. The rule audit measured 57 sites in the ahra tree and
	// the rule is not auto-fixable, so every one of them is a human decision about whether the
	// author meant "null or undefined" or meant "null". Turning it on at error makes 57 files fail
	// the gate until somebody works through them. That is a decision for Kirk rather than for this
	// port, and it is recorded here so the number is not discovered by a red build.
	"no-eq-null": "ported from eslint core, which marks it recommended:false; not enforced by either " +
		"tool when the inventory was captured, and the audit measured 57 violations it will now " +
		"require somebody to work through by hand",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit measured three sites in ahra and rated the rule Maybe, so this is a small cleanup
	// rather than a guardrail. Two of the three are the same shape, `(bucket ??= {})` inside a
	// declarator, which is a chain upstream reports because ESTree's AssignmentExpression covers all
	// sixteen assignment operators rather than only `=`. That widening is the largest gap between
	// this rule's corpus and its behaviour and it is measured rather than inferred.
	"no-multi-assign": "ported from eslint core, which marks it recommended:false; not enforced by " +
		"either tool when the inventory was captured, and the audit measured 3 violations",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit measured zero violations, so this is a guardrail against drift rather than a
	// cleanup, and the audit rated it Yes on exactly that basis.
	//
	// It reads the type checker, which upstream does not, and that is the port rather than an
	// addition. Upstream asks eslint-scope whether the name `Symbol` has any definition in source
	// and proceeds only when it has none. Four of its six clean cases are shadowing, so a rule
	// matching the name textually reports all four. `resolvesToAGlobal` asks the same question
	// through resolution and was probed against all eight upstream cases before being built on.
	"symbol-description": "ported from eslint core, which marks it recommended:false; not enforced " +
		"by either tool when the inventory was captured, and the audit measured zero violations",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit measured zero violations, so this is a guardrail against drift rather than a
	// cleanup, and the audit rated it Yes on exactly that basis. The tree writes almost no labels
	// at all.
	//
	// It reads the type checker, which upstream does not, and that is the port rather than an
	// addition. Upstream asks eslint-scope for a variable of the label's name walking outward to the
	// global scope; `GetSymbolsInScope` asks the same question, and the meaning it is asked for is
	// what had to match. `SymbolFlagsValue` was measured against sixteen inputs on the installed
	// build rather than chosen, because upstream clashes with a function, a class, a let and a
	// const, not only with a var.
	//
	// Two upstream behaviours are reproduced rather than improved on, and both read as false
	// positives: a label named `Object` or `undefined` REPORTS, because the scope chain ends at the
	// global scope where every standard library name is a variable.
	"no-label-var": "ported from eslint core, which marks it recommended:false; not enforced by " +
		"either tool when the inventory was captured, and the audit measured zero violations",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit measured zero violations, so this is a guardrail against drift rather than a
	// cleanup, and the audit rated it Yes on exactly that basis. The tree writes almost no labels.
	//
	// The port carries upstream's fixer including the four cases it refuses, where a comment sits
	// inside the span the removal would delete. One divergence is recorded and it comes from the
	// parser rather than from the rule: our parser recovers from a `continue` naming a switch and
	// from a duplicate label, both of which eslint rejects as syntax errors, so the rule judges
	// inputs upstream never reaches. Sixteen well formed inputs were measured against the installed
	// rule and all sixteen agree.
	"no-extra-label": "ported from eslint core, which marks it recommended:false; not enforced by " +
		"either tool when the inventory was captured, and the audit measured zero violations",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit measured zero violations, so this is a guardrail against drift rather than a
	// cleanup, and the audit rated it Yes on exactly that basis.
	//
	// It replaces eslint-scope with the tree, and what upstream's `reference.from === scope` MEANS
	// was measured against the installed rule rather than read off its source. It is the innermost
	// eslint scope, which eslint opens for a block, a switch, a loop body, a catch and a `with` but
	// not for an unbraced if consequent, a labeled statement or any expression nesting, so several
	// shapes report on code where the alias does end up holding `this`. Reproduced rather than
	// corrected, with the measurement at the line.
	//
	// Three behaviours the imported corpus could not show, each found by measuring: a function
	// expression is a scope of its own, an arrow is neither a scope nor transparent, and a
	// function's own body block is that function's scope rather than a nested one.
	"consistent-this": "ported from eslint core, which marks it recommended:false; not enforced by " +
		"either tool when the inventory was captured, and the audit measured zero violations",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit measured zero violations and that zero has a cause worth stating: two of the three
	// ported judgments fire only in a SCRIPT, and a scan of this tree found 3174 files carrying a
	// top level import or export against 2 that do not. So it is a guardrail rather than a cleanup,
	// and the fixtures prove the rule can fire rather than leaving the zero to speak for itself.
	//
	// Three of upstream's five messages are ported. The other two need a configured-globals surface,
	// declined here on the same measured grounds as `@typescript-eslint/no-redeclare`'s
	// `builtinGlobals`. The decline is an upgrade that does not happen rather than a finding that
	// goes missing, except for one shape, and all of it is pinned by a test.
	"no-implicit-globals": "ported from eslint core, which marks it recommended:false; not enforced " +
		"by either tool when the inventory was captured, and the audit measured zero violations",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// This one is a real cleanup rather than a guardrail. The audit measured 172 violations and this
	// port measures 168 across the tree, none auto-fixable, since upstream ships a SUGGESTION rather
	// than a fix: adding a radix changes what a call returns for any string whose base was being
	// inferred, so a human has to choose it per site. That is a decision for Kirk rather than
	// something this port should quietly land, and it is called out in the commit.
	//
	// One deliberate divergence, in the direction of catching more. Upstream asks eslint-scope for
	// the PROGRAM scope's parseInt, so a block-scoped shadow anywhere in the file exempts every
	// call; this port resolves per call site, so `{ let parseInt; } parseInt("10")` reports here and
	// is clean upstream. Both answers are stated in a fixture.
	// Ported from eslint core and registered by this port, but enabled in neither engine. Outside
	// the inventory for the usual reason: upstream marks it `recommended: false`, so a project on
	// the recommended set never had it, and there is no oxlint config in the tree to have carried
	// it either.
	//
	// Left off because the live config already says off, at VerifySettings.json:362, in the same
	// block as react/jsx-key. That decision predates the port, and enabling it here would reverse
	// somebody's standing decision through the porting process. The full reasoning is in
	// `deliberatelyNotEnabled` in live_wiring_test.go.
	//
	// The audit measured 3 violations, all auto-fixable, so the cost of turning it on later is one
	// config line and a reviewed fix pass.
	"no-useless-rename": "ported and registered but left off, matching an explicit off in the live " +
		"config; not enforced by either tool when the inventory was captured, and the audit " +
		"measured 3 violations, all auto-fixable",

	"radix": "ported from eslint core, which marks it recommended:false; not enforced by either " +
		"tool when the inventory was captured, and the audit measured 172 violations while this " +
		"port measures 168, each needing a per-site decision because upstream offers a suggestion " +
		"rather than a fix",

	// Ported from eslint core and registered by this port, but enabled in neither engine. Outside
	// the inventory for the usual reason: upstream marks it `recommended: false`, so a project on
	// the recommended set never had it, and there is no oxlint config in the tree to have carried
	// it either.
	//
	// Left off for a reason that is a measurement rather than a
	// preference: `osvfs` strips a leading byte order mark before any rule runs, so this rule
	// cannot see the one thing it judges. The full measurement, with both controls, is in
	// `deliberatelyNotEnabled` in live_wiring_test.go and in the rule's own doc comment.
	//
	// The audit rated it Yes on the strength of zero violations in the tree. That zero is real and
	// it is also what the rule would report on a tree full of marks, which is why it is not
	// evidence here.
	"unicode-bom": "ported from eslint core, which marks it recommended:false; registered but left " +
		"off because osvfs strips the leading byte order mark before any rule runs, so the rule " +
		"cannot see its own subject; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint and registered, but NOT enabled and therefore NOT given an
	// inventory entry, because an entry says the gate being replaced enforces it and nothing does.
	// The reason it is off is volume: 2,846 findings across 672 files measured on the ahra tree
	// against the installed 8.67.0 build, no fixer, and every site a judgment about what the code
	// really guarantees. That is a decision with an owner, not a porting question.
	"@typescript-eslint/no-unsafe-type-assertion": "ported from typescript-eslint; registered but " +
		"left off pending a decision about volume, measured at 2,846 findings across 672 files on " +
		"the ahra tree with no fixer and a human judgment at every site; not enforced by either " +
		"tool when the inventory was captured",

	// Ported from typescript-eslint and registered, but not enabled and therefore given no inventory
	// entry, because an entry asserts the gate being replaced enforces it and the config says the
	// opposite. VerifySettings.json:366 turns it off under the old short spelling, which the
	// resolver cannot match against the full registered name, so enabling would reverse that
	// decision through a spelling difference rather than because anybody changed their mind.
	"@typescript-eslint/restrict-template-expressions": "ported from typescript-eslint; registered " +
		"but left off because the live config carries a prior off for it under the old short " +
		"spelling, which cannot resolve against the full name; not enforced by either tool when " +
		"the inventory was captured",

	// Ported from typescript-eslint and registered, but not enabled and therefore given no inventory
	// entry, for the same reason as the entry above. VerifySettings.json:373 turns it off under the
	// old short spelling, which the resolver cannot match against the full registered name.
	"@typescript-eslint/no-useless-default-assignment": "ported from typescript-eslint; registered " +
		"but left off because the live config carries a prior off for it under the old short " +
		"spelling, which cannot resolve against the full name; not enforced by either tool when " +
		"the inventory was captured",

	// The same shape as the entry above. VerifySettings.json:372 carries the prior off under the
	// old short spelling, and the reason it cannot resolve is recorded in full beside this rule's
	// entry in the live-wiring guard's exemption map.
	"@typescript-eslint/no-duplicate-type-constituents": "ported from typescript-eslint; registered " +
		"but left off because the live config carries a prior off for it under the old short " +
		"spelling, which cannot resolve against the full name; not enforced by either tool when " +
		"the inventory was captured",

	// The third of this shape. VerifySettings.json:369 carries the prior off under the old short
	// spelling, and the reason it cannot resolve, including the three lines the linter itself prints
	// about the orphaned key, is recorded beside this rule's entry in the live-wiring exemption map.
	//
	// Upstream marks it `recommended`, so the tool being replaced would have carried it had the key
	// resolved. The audit measured eighteen violations and the rule ships no fixer, so enabling it is
	// eighteen hand decisions rather than a fix run, which is a decision for whoever wrote that off.
	"@typescript-eslint/unbound-method": "ported from typescript-eslint, which recommends it; " +
		"registered but left off because the live config carries a prior off for it under the old " +
		"short spelling, which cannot resolve against the full name; the audit measured 18 " +
		"violations, none auto-fixable",

	// The fourth of this shape, and the reason is recorded beside its entry in the live-wiring
	// exemption map. Upstream marks it `recommended`, so the tool being replaced would have carried
	// it had the key resolved.
	//
	// The audit measured 53 violations and rates it Yes, while noting that several sites are
	// deliberate String() fallbacks in generic serializers that already branch on typeof. So enabling
	// it is a judgment about those sites rather than a cleanup, and the rule ships no fixer.
	"@typescript-eslint/no-base-to-string": "ported from typescript-eslint, which recommends it; " +
		"registered but left off because the live config carries a prior off for it under the old " +
		"short spelling, which cannot resolve against the full name; the audit measured 53 " +
		"violations, none auto-fixable",

	// The first rule from api-phi-health's own `base` lint layer, which is a different kind of entry
	// from every one above it. Those are ports of somebody else's published rule, measured against an
	// upstream that ships a corpus. This is one of ours: the source repository is the only oracle,
	// and there is no inventory entry because the gate being replaced never enforced it here.
	//
	// Registered and not enabled, for the same reason as every other base/ rule here: ahra's config
	// names no base/ keys, and the inventory was captured from ahra's own gate, which has no base
	// surface to have enforced this. The judgment it ports is real and measured against the ESLint
	// original in api-phi-health; where it runs is a decision nobody has made.
	"base/graphql-operation-context-matches-return": "ported from api-phi-health's own base lint " +
		"layer; registered but not enabled because ahra's config names no base/ rules at all, so " +
		"where it is enforced is a pending decision rather than one this port should make",

	// Registered and not enabled, same reason as its sibling above.
	"base/graphql-nullable-parity": "ported from api-phi-health's own base lint layer; registered " +
		"but not enabled because ahra's config names no base/ rules at all, so where it is " +
		"enforced is a pending decision rather than one this port should make",

	// Registered and not enabled, because ahra's config names no base/ rules at all. See its entry in
	// the live-wiring exemption map for why that is a pending decision rather than an oversight.
	"base/no-global-container": "ported from api-phi-health's own base lint layer; registered but " +
		"not enabled because ahra's config names no base/ rules at all, so where it is enforced is " +
		"a decision nobody has made yet; the gate being replaced never enforced it here",

	// The second rule from api-phi-health's own base lint layer, and the first type-aware one. Same
	// reasoning as its sibling above: no inventory entry because the gate being replaced never
	// enforced it here, and not enabled because ahra's config names no base/ rules.
	"base/relation-must-be-optional": "ported from api-phi-health's own base lint layer; " +
		"registered but not enabled because ahra's config names no base/ rules at all, so where it " +
		"is enforced is a decision nobody has made yet; the gate being replaced never enforced it here",

	// The third rule from api-phi-health's own base lint layer. Same reasoning as its two siblings:
	// no inventory entry because the gate being replaced never enforced it here, and not enabled
	// because ahra's config names no base/ rules.
	"base/orm-column-requires-declare": "ported from api-phi-health's own base lint layer; " +
		"registered but not enabled because ahra's config names no base/ rules at all, so where it " +
		"is enforced is a decision nobody has made yet; the gate being replaced never enforced it here",

	// The same shape, and its port is likewise checked against the source repository rather than an
	// imported corpus. The original rule was driven over all 2,556 TypeScript files of
	// api-phi-health through the ESLint Linter API, and its net verdict there is zero: eight sites
	// reach the rule and all eight carry an author-written disable with a stated reason.
	//
	// That zero needed separating from a rule that never looked, and stripping the disable
	// directives is what did it: the same run then reports all eight, in the two files the exemption
	// paths do not cover. The fixtures are the original's verdicts on thirty-nine designed inputs,
	// twenty-two of which exist to prove a gate declines.
	//
	// The gates are all substring or suffix tests on a filename, which widen easily and silently, so
	// four near misses are pinned as REPORTING rather than left implied: `source/latest/` and
	// `source/protest/` both contain the letters of a test directory, `schema-tools` looks like the
	// schema builder, and `NotBaseError.ts` ends in a capture-path filename without its separator.
	// Mutating either gate into its wider spelling fails those rows.
	"base/no-bare-throw": "ported from api-phi-health's own base lint layer; registered but not " +
		"enabled because ahra's config names no base/ rules at all, so where it is enforced is a " +
		"decision nobody has made yet; the gate being replaced never enforced it here",

	// The same shape as the entry above and for the same reason. Its port is checked against the
	// source repository rather than an imported corpus: run over all 1,814 TypeScript files of
	// api-phi-health's base library, it agrees with the real rule on every unsuppressed site.
	"base/no-console": "ported from api-phi-health's own base lint layer; registered but " +
		"not enabled because ahra's config names no base/ rules at all, so where it is enforced is " +
		"a decision nobody has made yet; the gate being replaced never enforced it here",

	// The two nullability parity rules, checked against the real rules driven over the eslint
	// interface rather than an imported corpus. They share their judgment through
	// `internal/utilities/ecmascript/decorators` rather than importing each other, which the leaf
	// guard requires and which the other two parity rules in this family also use.
	"base/orm-column-nullable-parity": "ported from api-phi-health's own base lint layer; registered but " +
		"not enabled because ahra's config names no base/ rules at all, so where it is enforced is " +
		"a decision nobody has made yet; the gate being replaced never enforced it here",
	"base/serializable-nullable-parity": "ported from api-phi-health's own base lint layer; registered but " +
		"not enabled because ahra's config names no base/ rules at all, so where it is enforced is " +
		"a decision nobody has made yet; the gate being replaced never enforced it here",

	// The same shape as the entries above and for the same reason, and its oracle is the sharpest of
	// them. api-phi-health reports zero for this rule across its 2310 linted files, which is a real
	// property of that tree rather than a broken instrument: the same run reports 44 findings from
	// other rules. So the check is a SEEDED file placed inside that project's tsconfig include,
	// where the original and this port agree on seven reporting shapes and nine clean ones, line and
	// column, including the two whose guards prevent a panic rather than a wrong verdict.
	// The largest of the base rules, and the only one of them that is LIVE judgment in the source
	// repository: it is one of four base rules actually enabled in `BaseLintConfiguration.ts`. It
	// also carries no default configuration, so it enforces nothing until a project supplies the
	// protected keys and the decorators that unlock them, which is a second reason it is registered
	// and not enabled here.
	//
	// Its oracle is the source rule itself, loaded into the ESLint Linter with that live wiring and
	// driven over forty-six inputs plus a sweep of all 2,293 TypeScript files of api-phi-health.
	// That sweep reports ZERO, which is a real property rather than a broken instrument: a seeded
	// violation placed in the same run reports, and this port agrees with the original on it to the
	// line and column.
	"base/context-requires-access": "ported from api-phi-health's own base lint layer; registered " +
		"but not enabled because ahra's config names no base/ rules at all, so where it is " +
		"enforced is a decision nobody has made yet; the gate being replaced never enforced it here",

	"base/pagination-decorator": "ported from api-phi-health's own base lint layer; registered but " +
		"not enabled because ahra's config names no base/ rules at all, so where it is enforced is " +
		"a decision nobody has made yet; the gate being replaced never enforced it here",

	// The same shape as the entries above, with the strongest oracle of the four. This one was
	// checked against api-phi-health's ACTUAL Provider decorator and TypedInjectionKey rather than a
	// synthetic stand-in, on a file seeded inside that project's tsconfig include, and the rendered
	// types matched character for character including `string | ObjectFactory<string> | undefined`.
	//
	// It is the first base rule here that reads the type checker. Every judgment it makes is a
	// checker question, so the plain harness would leave it silently and vacuously green, which a
	// fixture asserts against directly.
	// The two Verify parity rules, ported together because they share their anchor: both judge a
	// class property or a constructor parameter property, and both read the property key's type
	// through the checker. The judgments differ entirely, so what they share is the traversal and
	// the shared decorator utility rather than any decision.
	//
	// Both were checked against the REAL ESLint rules driven over api-phi-health, and that oracle
	// earned its place: it corrected a fixture asserting that a `VerifyBy` decorator suppresses
	// array parity, which it does not, and it settled that a non-Verify decorator beside a
	// validation rule still reports.
	"base/verify-optional-parity": "ported from api-phi-health's own base lint layer; " +
		"registered but not enabled anywhere, because that project has no VerifySettings.json and " +
		"ahra's config names no base/ rules; which trees enforce it is a decision nobody has made yet",

	"base/verify-array-parity": "ported from api-phi-health's own base lint layer; " +
		"registered but not enabled anywhere, for the same reason as its sibling above",

	"base/provider-return-matches-token": "ported from api-phi-health's own base lint layer; " +
		"registered but not enabled because ahra's config names no base/ rules at all, so where it " +
		"is enforced is a decision nobody has made yet; the gate being replaced never enforced it here",

	// The same shape as the two entries above and for the same reason. Its port is checked against
	// the source repository rather than an imported corpus: driven through the real rule loaded out
	// of api-phi-health, thirteen invented shapes were measured rather than assumed, including four
	// the original knowingly misses.
	// The first type-aware base rule I have taken. Same reasoning as its siblings, and its port is
	// checked the same way: twelve invented shapes driven through the real rule with a real program,
	// including three placements on non-parameters that a mutant would otherwise have reached.
	"base/inject-type-matches-parameter": "ported from api-phi-health's own base lint layer; " +
		"registered but not enabled because ahra's config names no base/ rules at all, so where it " +
		"is enforced is a decision nobody has made yet; the gate being replaced never enforced it here",

	"base/no-hand-built-declared-error": "ported from api-phi-health's own base lint layer; " +
		"registered but not enabled because ahra's config names no base/ rules at all, so where it " +
		"is enforced is a decision nobody has made yet; the gate being replaced never enforced it here",

	// Ported from typescript-eslint and enabled in both engines by this port. Unlike the entry above
	// it carries no prior decision in the config to override, so enabling is the ordinary path: the
	// audit measures zero violations, which makes it a guardrail against drift rather than cleanup.
	// Outside the inventory for the usual reason, that neither tool enforced it at capture time.
	"@typescript-eslint/prefer-find": "ported from typescript-eslint, whose stylistic preset carries it; not enforced by either tool when the inventory was captured",

	// Ported from typescript-eslint and enabled in both engines by this port. No prior decision in
	// the config to override: neither spelling of the key appears there, confirmed against the
	// linter's own orphaned-key report rather than by grep alone. Outside the inventory for the
	// usual reason, that neither tool enforced it when the capture was taken.
	//
	// Worth naming the cost: the audit measures 43 sites, so enabling this is real cleanup rather
	// than a guardrail, though the rule is auto-fixable at most of them.
	"@typescript-eslint/consistent-indexed-object-style": "ported from typescript-eslint, whose stylistic preset carries it; not enforced by either tool when the inventory was captured",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// This one turns on with real cleanup attached rather than as a guardrail, and the number is
	// the decision rather than a footnote: 20 files in this tree hold more than one class, ranging
	// from two up to seven, and the rule ships no fixer because the repair is a human judgment
	// about which class moves to which new file under what name. The audit rated it Maybe on
	// exactly that basis and measured 18; the two extra are drift since that snapshot rather than a
	// disagreement.
	//
	// The count was cross-checked rather than trusted: the same 20 files were run through the
	// installed eslint with this rule alone, and it reports every one of them. An earlier run of
	// that check said zero on all 20, which was a missing `files` pattern in the probe rather than
	// a divergence, and it is recorded here because a zero from a broken instrument reads exactly
	// like agreement.
	"max-classes-per-file": "ported from eslint core, which marks it recommended:false; not " +
		"enforced by either tool when the inventory was captured, and the audit measured 18 " +
		"violations while this port measures 20, each needing a file split by hand",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// THIS IS THE LARGEST CLEANUP IN THIS BATCH BY A WIDE MARGIN and the number belongs in front of
	// whoever owns the config rather than in a footnote: 366 findings across 157 files, with one
	// file carrying 21. The audit rated it Yes and predicted 363, so the count is expected rather
	// than a surprise, and it was cross-checked rather than trusted: the installed eslint reports
	// 366 over the same 157 files with zero per-file disagreements.
	//
	// What the number does not say, and a reader deciding about this rule needs to know: the rule
	// cannot distinguish an accidental serialization from a deliberate one, and upstream says so by
	// shipping no fixer and marking itself not recommended. Findings were read rather than counted.
	// A retry loop awaiting a backoff delay and a pagination loop awaiting the page that carries the
	// next cursor are both true positives by the rule's definition and both are correct code where
	// the sequencing is the point. Turning this off in those places is a per-site judgment, which is
	// the cost the 366 actually represents.
	"no-await-in-loop": "ported from eslint core, which marks it recommended:false; not enforced " +
		"by either tool when the inventory was captured, and the audit measured 363 violations " +
		"while this port measures 366 across 157 files, each needing a per-site judgment",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The cheapest rule in this batch to turn on: 4 findings, which is exactly the four sites the
	// audit named, and the installed eslint reports the same four at the same line and column. Each
	// is a string split across a `+` for line-length reasons, so the repair is joining two literals
	// and there is no fixer because upstream ships none and the join is not always mechanical.
	"no-useless-concat": "ported from eslint core, which marks it recommended:false; not enforced " +
		"by either tool when the inventory was captured, and the audit measured 4 violations, " +
		"which this port reproduces at the same four positions",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// 16 findings across 9 files, exactly what the audit measured, and the installed eslint reports
	// the same 16 at the same line and column. The rule is fixable, which makes the interesting
	// number the second one: only 7 of the 16 carry a repair, and this port declines the same 9
	// eslint declines, file by file. That agreement is on real code rather than on the corpus, and
	// it is the number that matters here because a fix is applied unattended and the engine's only
	// guard is that the result parses, which every one of these declines would have passed.
	"no-lonely-if": "ported from eslint core, which marks it recommended:false; not enforced by " +
		"either tool when the inventory was captured, and the audit measured 16 violations, which " +
		"this port reproduces at the same positions with the same 7 repairs and 9 declines",

	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// 50 findings across 19 files, exactly what the audit measured, and the installed eslint reports
	// the same 50 at the same line and column. It is fixable, so the number that matters more is
	// the rewrite: this port's fixes were applied to all 19 real files and the resulting bytes
	// match eslint's own output byte for byte, including the one finding both tools decline.
	//
	// Its fixer found a defect no upstream fixture could contain. `as` and `satisfies` bind LOOSER
	// than every arithmetic operator, and upstream's precedence table has no row for either because
	// its corpus is JavaScript, so a port inheriting that table ranks them tightest and expands
	// `x += 1 as number` to `x = x + 1 as number`, which asserts the type of the sum rather than of
	// the addend. Caught by comparing the two parses directly, and pinned by a fixture that fails
	// when the precedence row is reverted.
	"operator-assignment": "ported from eslint core, which marks it recommended:false; not " +
		"enforced by either tool when the inventory was captured, and the audit measured 50 " +
		"violations, which this port reproduces at the same positions and rewrites identically",
	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit rated it Yes and measured 61 sites, which makes this a cleanup rather than a
	// guardrail, and the rule catches a defect rather than a preference: the Promise constructor
	// discards whatever its executor returns, so a returned value is dead at best and a missing
	// resolve at worst. It ships suggestions rather than a fix, matching upstream, because both
	// repairs change what the code means.
	"no-promise-executor-return": "ported from eslint core, which marks it recommended:false; not " +
		"enforced by either tool when the inventory was captured, and the audit measured 61 " +
		"violations it will now require somebody to work through by hand",
	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit measured zero violations, so this is a guardrail against drift rather than a
	// cleanup, and the audit rated it Yes on exactly that basis.
	//
	// One option is deliberately not ported and the decline is recorded here as well as at the line,
	// because this is where a coverage audit looks. `enforceForTSTypes` extends the same judgment to
	// accessor signatures in a TypeScript type literal or interface body. It defaults to FALSE, so
	// declining it is what an unset project already gets, and the live config names it nowhere.
	// Upstream's 14 cases for it are imported and pinned as clean rather than dropped.
	"grouped-accessor-pairs": "ported from eslint core, which marks it recommended:false; not " +
		"enforced by either tool when the inventory was captured, and the audit measured zero " +
		"violations; upstream's enforceForTSTypes option is declined, see the comment above",
	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit rated it Maybe and measured 45 sites, so this is a cleanup rather than a guardrail,
	// and the repair is a suggestion rather than a fix: writing a comment into an empty body asserts
	// that the emptiness is deliberate, which is a claim about intent that nothing may make
	// unattended.
	//
	// Worth recording because it changes what the typescript-eslint extension is for. That extension
	// reads as though it adds the TypeScript kinds, and measured against eslint 10.8.1 it does not:
	// core already exempts a private constructor, a protected constructor, a constructor taking
	// parameter properties, a decorated method and an override method. The only real difference is
	// spelling, `private-constructors` against `privateConstructors`, and core refuses the kebab
	// form at config load. This port is the core rule and refuses it too.
	"no-empty-function": "ported from eslint core, which marks it recommended:false; not enforced " +
		"by either tool when the inventory was captured, and the audit measured 45 violations it " +
		"will now require somebody to work through by hand",
	// Ported from eslint core and enabled in both engines by this port. Outside the inventory for
	// the usual reason: upstream marks it `recommended: false`, so a project on the recommended set
	// never had it, and there is no oxlint config in the tree to have carried it either.
	//
	// The audit measured one violation, so this is very nearly a guardrail. It is the second of the
	// two extension-rule cores: the typescript-eslint rule of the same name fetches this one and
	// filters on top, so it could not be ported until this existed.
	//
	// The repair is a suggestion rather than a fix, matching upstream, because deleting a
	// constructor changes what the class declares. Its one genuinely delicate part is that removing
	// a constructor can require leaving a semicolon behind, since a class field written without one
	// is terminated by automatic semicolon insertion and can run into the member that follows.
	"no-useless-constructor": "ported from eslint core, which marks it recommended:false; not " +
		"enforced by either tool when the inventory was captured, and the audit measured 1 violation",
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

// TestInventoryCountersMatchItsOwnRules keeps the summary counters honest without asking a porter
// to maintain them.
//
// `rule-inventory.json` opens with `total`, `byNamespace`, `bySeverity` and `byEnabledBy`. Nothing
// reads any of them: the parity comparison unmarshals `rules[].rule` and ignores the rest. So they
// are documentation that sits at the top of the file a reader opens first, which is the position
// most likely to be believed and least likely to be checked.
//
// They were also four hand-edits per port, and the shape of the mistake is quiet. A porter who
// appends an entry and forgets `total` leaves a file whose header disagrees with its body, and no
// test says so. During a porting wave that is a near certainty rather than a risk.
//
// So this derives them and compares. The fix on failure is to correct the counters, never to relax
// this: a counter that is allowed to be wrong is worse than one that is absent, because absence is
// visible and wrongness reads as measurement.
func TestInventoryCountersMatchItsOwnRules(t *testing.T) {
	path := inventoryPath()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}

	var document struct {
		Total       int            `json:"total"`
		ByNamespace map[string]int `json:"byNamespace"`
		BySeverity  map[string]int `json:"bySeverity"`
		ByEnabledBy map[string]int `json:"byEnabledBy"`
		Rules       []struct {
			Rule      string `json:"rule"`
			Severity  string `json:"severity"`
			EnabledBy string `json:"enabledBy"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	if len(document.Rules) == 0 {
		t.Fatalf("%s parsed to zero rules, so this would pass by having nothing to compare", path)
	}

	// A bare name is a core rule, and the inventory files those under "(core)" rather than under a
	// namespace. Read out of the file rather than assumed: the first version of this guard wrote
	// "eslint" and the guard itself reported the disagreement, which is the behaviour it exists for.
	namespaceOf := func(rule string) string {
		if index := strings.Index(rule, "/"); index >= 0 {
			return rule[:index]
		}
		return "(core)"
	}

	wantNamespace := map[string]int{}
	wantSeverity := map[string]int{}
	wantEnabledBy := map[string]int{}
	for _, entry := range document.Rules {
		wantNamespace[namespaceOf(entry.Rule)]++
		if entry.Severity != "" {
			wantSeverity[entry.Severity]++
		}
		if entry.EnabledBy != "" {
			wantEnabledBy[entry.EnabledBy]++
		}
	}

	if document.Total != len(document.Rules) {
		t.Errorf("the inventory says total %d and carries %d rules", document.Total, len(document.Rules))
	}
	compare := func(label string, want map[string]int, got map[string]int) {
		for key, count := range want {
			if got[key] != count {
				t.Errorf("%s[%q] says %d, the rules array holds %d", label, key, got[key], count)
			}
		}
		for key, count := range got {
			if _, present := want[key]; !present {
				t.Errorf("%s[%q] says %d, and no rule in the array has that value", label, key, count)
			}
		}
	}
	compare("byNamespace", wantNamespace, document.ByNamespace)
	compare("bySeverity", wantSeverity, document.BySeverity)
	compare("byEnabledBy", wantEnabledBy, document.ByEnabledBy)
}
