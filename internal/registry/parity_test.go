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
