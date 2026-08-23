package rule

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Registration is one rule plus everything the tool needs to know about it.
//
// A rule's options decoder lives here rather than on Rule itself, because Rule is the type a rule
// author writes and a decoder is the config layer's business. Keeping them in one Registration
// means a rule is declared in exactly one place while neither package has to import the other: the
// rule package builds the decoder with a generic helper and hands it over as an opaque function.
type Registration struct {
	Rule Rule
	// Decode turns this rule's raw JSON options into its own options struct, or nil if the rule
	// takes no options, which is the common case.
	Decode func(raw []byte) (any, error)
	// RequiresOptions is whether the rule declines every file without its options.
	//
	// The load-bearing field, and the whole lesson of the inert-rule defect.
	// `boundary-no-project-import` was enabled and inert for months, declining every file because
	// its configured directory was empty, which is correct behavior for a misconfigured guard and
	// indistinguishable from a rule with nothing to report. Marking it required turns that silence
	// into a failure.
	RequiresOptions bool
}

// registered is every rule any linked package has registered, keyed by name.
//
// Guarded by a mutex because `init` functions across packages are ordered but a test may register
// from a goroutine, and a map written concurrently crashes the process rather than corrupting
// quietly. The cost is paid once per rule at startup.
var (
	registeredMutex sync.Mutex
	registered      = map[string]Registration{}
)

// Register adds a rule to the catalog, and panics if the name is already taken.
//
// Called from each rule package's own `init`, which is what removes the shared file every port used
// to edit. Before this, adding a rule meant appending one line to a single sorted list in another
// package, so two ports landing at once conflicted on a file neither of them was really changing.
// Now a rule is two new files and no shared edit, which is the difference between porting a few
// rules at a time and porting a family at once.
//
// A duplicate name panics rather than overwriting. Two rules answering to one name is not a
// resolvable state: the config addresses rules by name, so whichever one lost would be silently
// unreachable while its tests kept passing, which is the same shape as the inert-rule defect this
// tool exists to catch. Failing at startup makes it a crash in the first second rather than a gap
// nobody measures.
func Register(registrations ...Registration) {
	registeredMutex.Lock()
	defer registeredMutex.Unlock()

	for _, registration := range registrations {
		name := registration.Rule.Name
		if name == "" {
			panic("rule.Register: a rule was registered with an empty name, so the config could never address it")
		}
		if existing, taken := registered[name]; taken {
			panic(fmt.Sprintf(
				"rule.Register: two rules answer to %q (%T and %T); the config addresses rules by "+
					"name, so one of them would be permanently unreachable while its tests passed",
				name, existing.Rule.Run, registration.Rule.Run,
			))
		}
		registered[name] = registration
	}
}

// Registered returns every registered rule, in a stable order.
//
// Order is by name, and it is stable so that two runs over the same tree report findings in the
// same sequence, which is what makes a diff between them meaningful. Sorting here rather than
// relying on registration order also removes the last thing that depended on `init` ordering across
// packages, which Go defines but which no reader should have to reason about.
func Registered() []Registration {
	registeredMutex.Lock()
	defer registeredMutex.Unlock()

	names := make([]string, 0, len(registered))
	for name := range registered {
		names = append(names, name)
	}
	sort.Strings(names)

	ordered := make([]Registration, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, registered[name])
	}
	return ordered
}

// DecodeOptionsInto builds the decoder a Registration carries, for any options struct.
//
// Used as `rule.DecodeOptionsInto[NoConcatenatedClassesOptions]()`, it lives here rather than in
// `internal/config` for a measured reason: a rule package that imports the config layer stops being
// a leaf of the import graph, and editing a rule then rebuilds in about 8.5s instead of about 1.8s.
// `TestRulePackagesStayLeaves` asserts that, and it caught this file importing config the first
// time registration moved into the rule packages.
//
// Nothing here is config-specific. It decodes JSON into a struct, which is what the rule declared
// and what only the rule's own package knows the type of. The config layer's job is deciding
// whether a rule that got no options may still run, and that judgment stays there.
func DecodeOptionsInto[Options any]() func(raw []byte) (any, error) {
	return func(raw []byte) (any, error) {
		var decoded Options
		if len(raw) == 0 {
			return decoded, fmt.Errorf("no options were configured")
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return decoded, fmt.Errorf("decoding %T: %w", decoded, err)
		}
		return decoded, nil
	}
}
