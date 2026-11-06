package changelog

// SetChanges is what changed in one set between two releases.
type SetChanges struct {
	// Added is a set that did not exist before, Removed one that no longer exists.
	Added   bool
	Removed bool

	// TurnedOn are rules that now report, with their severity; TurnedOff are rules that no longer do.
	TurnedOn  []string
	TurnedOff []string
	// Severity are rules that report at another severity, as "rule: before -> after".
	Severity []string
	// Options are rules that report at the same severity with other options.
	Options []string
	// Settings are the set's other keys that changed: format, ignorePatterns, overrides and the rest.
	Settings []string

	// Bump is the smallest version these changes allow.
	Bump Bump
}

// Empty reports a set that did not change.
func (changes SetChanges) Empty() bool {
	return !changes.Added && !changes.Removed && len(changes.TurnedOn)+len(changes.TurnedOff)+len(changes.Severity)+
		len(changes.Options)+len(changes.Settings) == 0
}
