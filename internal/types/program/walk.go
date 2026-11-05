package program

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/suppression"
)

// Result is the findings of one walk, and the coverage that produced them.
type Result struct {
	Diagnostics []rule.Diagnostic
	Coverage    Coverage

	// Timings is per-rule cost, populated only when the caller asked for it by setting
	// Graph.CollectTimings. Nil otherwise, so an ordinary run pays nothing for the instrument.
	Timings *Timings

	// FilesReplayed is how many files this walk served from the findings cache rather than walking
	// with every rule. Zero without a cache. The lint line reports it, so a reader can tell a walked
	// verdict from a remembered one.
	FilesReplayed int

	// TypeAwareRerun and ShapeKeyedRerun are how many of the replayed files ran their content-keyed and
	// their shape-keyed type-aware rules again, because something they import changed. The difference
	// between the two is what shapes saved.
	TypeAwareRerun  int
	ShapeKeyedRerun int

	// DesignSystemRerun is how many of the replayed files ran their design-system rules again, because a
	// stylesheet the design system read changed or the last run's could not be keyed.
	DesignSystemRerun int

	// DerivedRerun is how many of the replayed files ran their derived rules again, because a rule's program
	// fingerprint moved, or the file's type fingerprint did under a derived rule that reads types.
	DerivedRerun int

	// FilesOnForeignCheckers is how many files were walked on a checker other than the one that owns them:
	// taken by a worker whose own group was done, or every file under WalkOnForeignCheckers. See walkQueue.
	FilesOnForeignCheckers int

	// Adamic is each walked file's readiness measurement, by file name, when Graph.Readiness asked for one: what
	// the walk ran, with what the findings cache replayed for the rest. A file whose record is nil was never
	// measured, and reads unmeasured, not ready. See Readiness.
	Adamic map[string]*AdamicRecord

	// Notes is what each file's rules noted through rule.Context.Note, by file name. Kept per file and
	// per rule rather than summed, because the findings cache stores a file's notes beside its findings
	// and a refresh re-walks only some of a file's rules: a total could not be split back apart.
	Notes map[string]RuleNotes
}

// RuleNotes is one file's notes: a count per key, per rule.
type RuleNotes map[string]map[string]int

// rulesOffered is how many rules were offered at least one file.
func rulesOffered(offeredCounts map[string]int) int {
	offered := 0
	for _, count := range offeredCounts {
		if count > 0 {
			offered++
		}
	}
	return offered
}

// newRuleNameCatalog returns nil when no registry was supplied, which turns the check off: without
// one, every name would read as unknown.
func newRuleNameCatalog(registered []string, configurationKeys []string) *ruleNameCatalog {
	if len(registered) == 0 {
		return nil
	}
	catalog := &ruleNameCatalog{
		names:                make(map[string]bool, len(registered)+len(configurationKeys)),
		registeredByBareName: make(map[string]string, len(registered)),
	}
	for _, name := range registered {
		catalog.names[name] = true
		catalog.registeredByBareName[bareRuleName(name)] = name
	}
	for _, key := range configurationKeys {
		catalog.names[key] = true
	}
	return catalog
}

// housePlugins are the plugins that are ours, whose every rule cohere runs, so a name under one that
// cohere doesn't know is a typo or a stale prefix rather than a rule it hasn't ported.
var housePlugins = map[string]bool{"adamic": true, "base": true, "nexus": true, "structure": true}

// reportUnknownRuleReferences reports every rule name a disable or enable comment wrote that can only
// be a mistake, the way ESLint does: rule id the unknown name, anchored on the whole comment, message
// beginning with ESLint's own sentence so the two engines read alike (measured on 10.8.1). A name that is
// a real rule cohere doesn't run is counted into unrun instead, for a --verbose note, and never reported.
//
// Ruled by @system_cohere (#v1ah2qq): a repository written for ESLint names plugin rules cohere has not
// ported, 43 in trpc, 6 in excalidraw and 160 in TanStack/query, and reporting each as an error made the
// first run on someone else's code a wall of findings about cohere rather than about the code. So:
//   - a name cohere knows (a registered rule, a config key, or the twin spelling of one) is fine;
//   - an unprefixed name ESLint ships is a core rule cohere doesn't run: unrun;
//   - an unprefixed name ESLint does not ship is a typo, since ESLint's core is fully known: reported;
//   - a name under one of our own plugins is a typo or a stale prefix, since cohere is their reference
//     engine and runs every rule they have: reported, with the registered name when there is one;
//   - a name under any other plugin is a rule cohere doesn't run: unrun. It is not told cohere's rule of
//     the same bare name, which belongs to another plugin and may not be the same rule.
//
// Reported straight to the walk rather than through a rule's Report, so no directive can silence a
// finding about a directive that silences nothing.
//
// Mutates: unrun counts each reference to a real rule cohere does not run, for the --verbose note
func reportUnknownRuleReferences(sourceFile *ast.SourceFile, directives *suppression.Index,
	catalog *ruleNameCatalog, report func(rule.Diagnostic), unrun map[string]int) {
	if catalog == nil {
		return
	}
	for _, reference := range directives.RuleReferences() {
		if catalog.exists(reference.Name) {
			continue
		}
		plugin := pluginOf(reference.Name)
		if plugin == "" && rule.IsEslintCoreRule(reference.Name) || plugin != "" && !housePlugins[plugin] {
			unrun[reference.Name]++
			continue
		}
		description := fmt.Sprintf("Definition for rule '%s' was not found. A suppression naming it "+
			"silences nothing, so whatever it was written to allow is reported anyway or was never "+
			"reported at all.", reference.Name)
		if registered, found := catalog.registeredByBareName[bareRuleName(reference.Name)]; found {
			description += fmt.Sprintf(" cohere registers this rule as '%s'.", registered)
		}
		report(rule.Diagnostic{
			RuleName:   reference.Name,
			Range:      core.NewTextRange(reference.Position, reference.End),
			Message:    rule.Message{Id: "ruleNotFound", Description: description},
			SourceFile: sourceFile,
		})
	}
}

// sortDiagnostics puts a walk's findings in file order, then position, then rule and message.
//
// Without it the order was goroutine scheduling: three runs over one unchanged tree printed three
// orderings of one identical set, so two runs could not be diffed and a walk-order change read as a
// regression nobody caused (#zevtfkq). It was left unsorted once on the grounds that sorting costs
// something on a path over 3,481 files. That cost was never measured and is not what this pays:
// this sorts findings, not files, and the whole ahra tree returned 52 on 2026-10-01.
//
// File then position is the order ESLint and tsc print, so a reader comparing the engines reads the
// same sequence in both. Rule name, message id and description only break ties at one range, and
// they make the order total, so a rule that reports in map order cannot leak that order out.
//
// It is also the boundary the lint cache's note asks for (lint_cache.go, HashRuleSet): a warm run
// replaying stored findings and a cold run walking fresh return one sequence, and cache state cannot
// decide it. The fix phase is unaffected either way, because the edit engine orders its proposals
// itself before resolving overlaps.
func sortDiagnostics(diagnostics []rule.Diagnostic) {
	fileName := func(diagnostic rule.Diagnostic) string {
		if diagnostic.SourceFile == nil {
			return ""
		}
		return diagnostic.SourceFile.FileName().AsString()
	}
	slices.SortStableFunc(diagnostics, func(first, second rule.Diagnostic) int {
		return cmp.Or(
			cmp.Compare(fileName(first), fileName(second)),
			cmp.Compare(first.Range.Pos(), second.Range.Pos()),
			cmp.Compare(first.Range.End(), second.Range.End()),
			cmp.Compare(first.RuleName, second.RuleName),
			cmp.Compare(first.Message.Id, second.Message.Id),
			cmp.Compare(first.Message.Description, second.Message.Description),
		)
	})
}

// FileCrash is a file a rule panicked on, and the panic it raised.
//
// The cause is carried rather than summarized because the panic message is the only evidence of what
// went wrong: `Unhandled case in Node.Text: *ast.Token` names the defect precisely, and a count of
// crashes names nothing.
type FileCrash struct {
	FileName string
	Cause    error
}

// RuleCrash is one rule that panicked on one file, and the panic it raised.
type RuleCrash struct {
	RuleName string
	FileName string
	Cause    error
}

// anyRuleNeedsTypeChecker reports whether any of these rules declared that it reads the checker.
//
// Asked per file against the applicable set rather than once against the whole catalog, because
// applicability is per file: a type-aware rule that declines this file should not make it pay for a
// checker nobody will read.
func anyRuleNeedsTypeChecker(rules []rule.Entry) bool {
	for _, subject := range rules {
		if subject.NeedsTypeChecker {
			return true
		}
	}
	return false
}

// DeadSuppression is one directive that silenced nothing while a rule it names ran: where it is, and
// the rules it names (none for a blanket directive).
type DeadSuppression struct {
	File  string
	Line  int
	Rules []string
}

// namesOnlyUnrunRules reports whether every rule a directive named is one this run did not run.
//
// A blanket directive names no rules and silences everything in its scope, so it is never in this
// category: something ran, and it still withheld nothing. A directive naming a mix is not either,
// because at least one rule looked and declined to fire, which is the shape worth deleting.
func namesOnlyUnrunRules(directive *suppression.Directive, ranRule map[string]bool) bool {
	if len(directive.Rules) == 0 {
		return false
	}
	for _, named := range directive.Rules {
		if ranRule[bareRuleName(named)] {
			return false
		}
	}
	return true
}

// namesOnlyOffRules reports whether the config turns off, for this file, every rule a directive named.
//
// Such a directive silences nothing whether or not cohere ports the rules, because a rule that is off
// cannot report, so it is dead rather than waiting on a port. The excuse is for a rule the config
// would run if this binary had it. ESLint flagged 13 `no-await-in-loop` directives in ahra as unused
// and was right, while cohere excused them as unported (#ezhwsbc). One named rule the config would run
// keeps the excuse, because the directive may be load-bearing for that one.
//
// Looked up by the name the directive wrote, which is the name the config is keyed by.
func namesOnlyOffRules(directive *suppression.Directive, resolution configuration.Resolved) bool {
	if len(directive.Rules) == 0 {
		return false
	}
	for _, named := range directive.Rules {
		if status, _ := resolution.StatusOf(named); status != configuration.StatusScopedOff {
			return false
		}
	}
	return true
}

// bareRuleName drops a plugin prefix, so `structure/no-x` and `no-x` compare equal.
//
// Directives and registered rules both may carry the plugin that owns the rule, and not always the same
// way (`core/no-x` against a bare `no-x`), so both sides are compared bare. Comparing either raw files a
// dead directive as naming an unrun rule.
func bareRuleName(name string) string {
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		return name[slash+1:]
	}
	return name
}

// pluginOf is a rule name's plugin, everything before its last `/`, empty for a core rule:
// `@typescript-eslint/no-x` is `@typescript-eslint`, `@next/next/no-x` is `@next/next`.
func pluginOf(name string) string {
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		return name[:slash]
	}
	return ""
}

// tally reads what a file's directives actually did, after the walk.
//
// The reasonless count is per withheld finding rather than per directive, because that is the
// number that answers the question being asked: how much of what cohere chose not to tell you was
// silenced by someone who did not say why.
func tally(
	fileName string,
	directives *suppression.Index,
	ranRule map[string]bool,
	resolution configuration.Resolved,
) suppressionTally {
	counted := suppressionTally{}
	for index, directive := range directives.Directives() {
		applied := directives.AppliedCount(index)
		if applied == 0 {
			counted.unusedDirectives++
			if namesOnlyUnrunRules(directive, ranRule) && !namesOnlyOffRules(directive, resolution) {
				counted.unusedForUnrunRule++
				continue
			}
			counted.dead = append(
				counted.dead,
				DeadSuppression{File: fileName, Line: directive.Line, Rules: directive.Rules},
			)
			continue
		}
		counted.applied += applied
		if !directive.HasReason() {
			counted.appliedNoReason += applied
		}
	}
	return counted
}

// walk visits every node once, calling whatever listeners registered for its kind. listeners is indexed
// by kind and holds ast.KindCount entries.
func walk(node *ast.Node, listeners [][]func(node *ast.Node)) int {
	if node == nil {
		return 0
	}

	visited := 1
	for _, listener := range listeners[node.Kind] {
		listener(node)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		visited += walk(child, listeners)
		return false
	})

	return visited
}

// noConfiguration keys the one selection a walk with no configuration makes: every rule, with no options, so
// each runs as its Unconfigured (boundIn). An identity of its own, which no Config's resolution can share, since
// identities are compared by pointer.
var noConfiguration = &configuration.ResolutionIdentity{}

// ruleClassNames is the applicable rules' names in each findings-cache class. See CacheClasses and
// ShapeClasses.
type ruleClassNames struct {
	pure    []string
	typed   []string
	shaped  []string
	design  []string
	derived []string

	// derivedReadsTypes is whether any derived rule reads types, which puts the type fingerprint in the derived
	// key. See derivedKey.
	derivedReadsTypes bool

	// derivedRules is the derived rules themselves, in derived's order, for their program fingerprints. See
	// programFingerprints.
	derivedRules []rule.Entry
}

// boundIn is subject as options decided it, or its Unconfigured when options holds nothing for it, as for every
// rule of a walk with no configuration.
func boundIn(options map[string]rule.Bound, subject rule.Entry) rule.Bound {
	if bound, decided := options[subject.Name]; decided {
		return bound
	}
	return subject.Unconfigured
}

// optionsKey spells a rule's option elements as one string that no other list of elements shares: each
// element's bytes, then a separator no JSON value contains.
func optionsKey(elements []json.RawMessage) string {
	var key strings.Builder
	for _, element := range elements {
		key.Write(element)
		key.WriteByte(0)
	}
	return key.String()
}

// busiestQueue is the group with the most files left, nil when every group is done. Files left stand in for
// work left: a group's files are of a kind, so its count tracks its cost closely enough to pick a victim.
func busiestQueue(queues []*walkQueue) *walkQueue {
	var busiest *walkQueue
	most := 0
	for _, queue := range queues {
		if left := queue.remaining(); left > most {
			busiest, most = queue, left
		}
	}
	return busiest
}

// foreignQueue is the group a worker walks under WalkOnForeignCheckers: the next worker's that has a group,
// so every file is walked on a checker that does not own it. A worker with no group of its own has no
// checker to lend and walks nothing.
func foreignQueue(queues []*walkQueue, homeFiles []*ast.SourceFile, worker int) *walkQueue {
	if homeFiles[worker] == nil {
		return &walkQueue{}
	}
	for step := 1; step < len(queues); step++ {
		if next := (worker + step) % len(queues); homeFiles[next] != nil {
			return queues[next]
		}
	}
	return queues[worker]
}

// programFingerprintKey names one fingerprint: the rule, and its option elements as optionsKey spells them.
type programFingerprintKey struct {
	rule    string
	options string
}

// programFingerprintEntry is one fingerprint, computed by the first worker to ask while the rest wait on once.
// usable is false when the fingerprint panicked.
type programFingerprintEntry struct {
	once        sync.Once
	fingerprint [sha256.Size]byte
	usable      bool
}

// derivedKey is a file's key for its derived rules: each one's name and program fingerprint, in the order they
// apply, and the file's type fingerprint when any of them reads types. Zero when a derived rule has no
// fingerprint this run, which no lookup matches, and when no derived rule applies.
func derivedKey(
	classes *ruleClassNames,
	programFingerprints map[string][sha256.Size]byte,
	typeFingerprint [sha256.Size]byte,
) [sha256.Size]byte {
	if len(classes.derived) == 0 {
		return [sha256.Size]byte{}
	}
	hash := sha256.New()
	for _, name := range classes.derived {
		fingerprint, found := programFingerprints[name]
		if !found {
			return [sha256.Size]byte{}
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(fingerprint[:])
	}
	if classes.derivedReadsTypes {
		hash.Write(typeFingerprint[:])
	}
	var key [sha256.Size]byte
	copy(key[:], hash.Sum(nil))
	return key
}
