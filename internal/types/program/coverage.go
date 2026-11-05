package program

// Coverage is what a run actually looked at.
//
// This exists because "what did you find" is the less important half of a verdict and the only half
// most tools report. A run that found nothing and a run that checked nothing print the same green,
// and this project's history is a list of times that ambiguity cost days: a gate that linted zero
// files and exited 0, three configs that loaded no plugins, a filter that stopped reporting. Every
// result this package returns carries what it covered, so a caller that prints a verdict without
// also printing the population had to work at it.
type Coverage struct {
	// FilesInProgram is the whole type graph: our code plus every declaration it pulled in.
	FilesInProgram int

	// FilesWalked is how many files the rules actually visited.
	FilesWalked int

	// NodesVisited is how many AST nodes this walk touched, across every file.
	NodesVisited int

	// NodesReplayed is how many nodes the replayed files' recorded walks visited, which this walk did not touch:
	// a file whose every rule replayed, or whose walked rules listened to nothing. A count that added these to
	// NodesVisited said "2,372,633 nodes visited" on an edit run that walked a few hundred files (#kdee854).
	NodesReplayed int

	// RulesRun is how many rules ran: were offered at least one file, here or in a replayed verdict.
	//
	// Counted from what was offered rather than from the rules the walk was handed, which are every
	// registered rule. A config that turns most of them off left the footer saying "484 rules" when 118
	// ran (#v1ah2qq), the very gap between configured and checked this struct exists to show. A rule
	// offered files that it declined still ran: it looked and had nothing to listen for.
	RulesRun int

	// RulesOffered counts, per rule name, how many files the rule was actually handed. A rule that
	// was never wired is offered zero; a rule that was offered files and declined all of them is
	// offered many and listens to none. Those are opposite defects (nobody configured it, against
	// it is configured and satisfied), and before this existed the coverage note described both with
	// one sentence, so a satisfied rule read exactly like a dead one.
	RulesOffered map[string]int

	// RulesListening counts, per rule name, how many files that rule chose to listen to. A rule that
	// declined every file reads as zero here, which is the difference between "ran and found nothing"
	// and "never actually looked": the distinction a bare finding count erases.
	RulesListening map[string]int

	// RulesReporting counts, per rule name, how many findings that rule produced.
	//
	// Listening and reporting are different questions and only together do they say what a zero
	// means. A rule that listened to no files never looked. A rule that listened to thousands and
	// reported nothing looked at real code and had nothing to say, which is either a clean tree or a
	// rule that cannot see. Two false positives shipped past a full fixture pair and were caught
	// only by running against the tree, so the distinction is worth a counter rather than a habit.
	RulesReporting map[string]int

	// Suppressed is how many findings a disable comment withheld.
	//
	// This is the coverage discipline pointed the other way. Coverage stops a run that checked
	// nothing from printing the same green as a run that found nothing; this stops a run that hid
	// forty findings from printing the same green as a run that had none to hide. A suppression is
	// a decision, and a decision that leaves no trace in the output is indistinguishable from the
	// tool being blind.
	Suppressed int

	// SuppressedWithoutReason is how many of those withheld findings were silenced by a directive
	// that never said why.
	//
	// Measured rather than enforced, deliberately. Requiring a reason today would turn 281 working
	// suppressions red for no defect, so the number is printed every run instead, which is what
	// lets the convention be tightened later from evidence rather than from a guess.
	SuppressedWithoutReason int

	// UnusedSuppressionsForUnrunRules is the subset of UnusedSuppressions naming only rules this run
	// did not run.
	//
	// During the migration this is most of the number, and the two mean opposite things. A directive
	// that silenced nothing while its rule ran is dead scaffolding worth deleting. One naming a rule
	// cohere has not ported yet silenced nothing because nothing looked, and deleting it would strip
	// a suppression the gate still needs. Collapsing them tells a reader to delete comments that are
	// load-bearing today.
	UnusedSuppressionsForUnrunRules int

	// UnusedSuppressions is how many directives never withheld anything.
	//
	// An unused suppression is a rule scoped off a file that no longer needs it, and it is how a
	// codebase accumulates permanent exemptions nobody chose. It only means what it says after a
	// full run with every rule, so a caller running a filtered subset should not report it.
	UnusedSuppressions int

	// DeadSuppressions names each directive counted in UnusedSuppressions and not in
	// UnusedSuppressionsForUnrunRules: one that silenced nothing while a rule it names ran. A count
	// alone sends a reader linting files one at a time to find them, and a single file is the wrong
	// tool, because without the whole program the type-aware rules go quiet and every directive for
	// one of them reads as dead.
	DeadSuppressions []DeadSuppression

	// FilesCrashed names the files a rule panicked on, with the panic that ended each.
	//
	// Named rather than counted, because the panic message is the defect and a count of crashes is
	// not actionable. A file the linter could not process is not a file with nothing to report, and
	// keeping those apart is the whole job of this struct.
	FilesCrashed []FileCrash

	// RulesCrashed names each rule that panicked on a file, with the file and the panic.
	//
	// A rule's panic is contained to that rule: it stops hearing about the file, and every other rule
	// on the file runs to the end. Before this, one rule's panic cost every rule its verdict on the
	// file. prefer-arrow-callback's shared arm crashed 71 files that way, and all 446 rules lost those
	// files under an ordinary-looking summary (#qa9nttp). The crashed rule's verdict on the file is
	// still missing, which is why it is named rather than counted.
	RulesCrashed []RuleCrash

	// FilesIgnored is how many files an ignorePattern excluded from linting entirely.
	//
	// A file skipped by configuration and a file with no findings produce identical output
	// otherwise, which is the same ambiguity the coverage line exists to destroy. Exclusion is a
	// decision someone made, and a decision that leaves no trace is indistinguishable from the tool
	// never having looked.
	FilesIgnored int

	// RulesScopedOff counts, per rule name, how many files the config turned that rule off for.
	//
	// This is the narrower half of the same accounting. A rule can be enabled in the config, run on
	// the tree, and still be silent across a whole directory because an override scoped it off
	// there. Without this number that looks identical to a rule with nothing to report.
	RulesScopedOff map[string]int

	// RulesUnconfigured counts, per rule name, how many files a rule skipped because the config
	// never mentions it.
	//
	// Deliberately separate from RulesScopedOff. "Someone turned this off" and "nobody has said
	// whether this should run" are different facts, and a registered rule missing from the config
	// is a decision waiting to be made rather than one already made. Reporting them as one number
	// would describe a new rule as though someone had excluded it.
	RulesUnconfigured map[string]int

	// UnrunRuleReferences counts, by name, the rules disable and enable comments named that are real and
	// that cohere doesn't run: an ESLint core rule it hasn't ported, or another plugin's rule. Such a
	// directive silences nothing here, which is a note rather than a finding (#v1ah2qq).
	UnrunRuleReferences map[string]int
}

// NodesCovered is every node the verdict covers: the ones this walk touched and the ones replayed files'
// recorded walks did. It is what a walk with no cache would report as NodesVisited.
func (coverage Coverage) NodesCovered() int {
	return coverage.NodesVisited + coverage.NodesReplayed
}
