package program

import (
	"crypto/sha256"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// ruleSelection is what one resolution decides about the rule set: the rules that apply, each bound to its
// decoded options, and the rules left out and why.
//
// Shared by every file that resolved the same way, on one worker, so nothing may write into it after it
// is made, its options included: a rule reads its options and never writes them.
type ruleSelection struct {
	applicable []rule.Entry
	options    map[string]rule.Bound

	// optionsKeys is each applicable rule's option elements as the config wrote them, joined, which is
	// what the walk's program fingerprints are keyed on. One run decodes every option through one registry,
	// so equal bytes are equal options. See programFingerprintMemo.
	optionsKeys map[string]string

	// scopedOff is the rules the configuration turned off here, and unconfigured the rules it never
	// mentions. Both are counted for every file the selection serves.
	scopedOff    []string
	unconfigured []string

	// err is a rule's options failing to decode, which fails the run.
	err error

	// measureOnly is the cohere:adamic rules the chain leaves off or never names, run for readiness alone,
	// empty when the run measures no readiness. See Readiness.
	measureOnly map[string]bool

	// classes is the applicable rules' findings-cache classes by name, made on first use. See classNames.
	classes *ruleClassNames

	// slots is the worker's slot for each applicable rule, aligned with applicable, found on first use. See
	// fileDispatcher.slotsFor.
	slots []*ruleSlot

	// programFingerprints is each derived rule's program fingerprint under this selection's options, looked up
	// in the walk's memo on first use. See Graph.programFingerprints.
	programFingerprints map[string][sha256.Size]byte
}

// classNames returns the selection's cache classes, made once. Made per file they were about 470 MB of a
// cold ahra run's allocation (#9jpmqm9).
func (selection *ruleSelection) classNames() *ruleClassNames {
	if selection.classes == nil {
		pure, typeAware, design, derived, _ := CacheClasses(selection.applicable)
		shaped, typed := ShapeClasses(typeAware)
		selection.classes = &ruleClassNames{
			pure:         ruleNames(pure),
			typed:        ruleNames(typed),
			shaped:       ruleNames(shaped),
			design:       ruleNames(design),
			derived:      ruleNames(derived),
			derivedRules: derived,
		}
		for _, subject := range derived {
			if subject.NeedsTypeChecker || subject.ProgramReads&rule.ReadsModuleResolution != 0 {
				selection.classes.derivedReadsTypes = true
			}
		}
	}
	return selection.classes
}
