package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// liveIdentifiers is upstream's `State`: the set of values read, plus the names they carry.
//
// Two sets rather than one because a named binding is read by name across its versions. If any
// version of `x` is read, every version must be kept, since sweeping one would leave a later read
// resolving to nothing.
type liveIdentifiers struct {
	identifiers map[static_single_assignment.IdentifierId]bool
	named       map[string]bool
}

// reference marks a value as read.
func (identifiers *liveIdentifiers) reference(function *Function, id static_single_assignment.IdentifierId) {
	identifiers.identifiers[id] = true
	if identifier := function.Identifiers[id]; identifier != nil && identifier.Name != "" {
		identifiers.named[identifier.Name] = true
	}
}

// usedByIdOrName reports whether this value, or any version of its name, is read.
//
// # A mutation making this ignore the name set survives
//
// Measured: id-only liveness scores identically -- goldens 25, ours 120, exact 20. No corpus fixture
// currently has one version of a named binding read while another is swept, because the sweep runs
// immediately after `DropManualMemoization` and the instructions it removes are freshly lowered
// temporaries with no name at all.
//
// Kept because it is the guard against the one unrecoverable failure this pass has: sweeping a
// version whose name is read later leaves that read resolving to nothing, which is a miscompile
// rather than a lost optimisation. The verdict expires the moment this runs anywhere later in the
// pipeline, where named reassignments are live across versions.
func (identifiers *liveIdentifiers) usedByIdOrName(function *Function, id static_single_assignment.IdentifierId) bool {
	if identifiers.identifiers[id] {
		return true
	}
	identifier := function.Identifiers[id]
	return identifier != nil && identifier.Name != "" && identifiers.named[identifier.Name]
}

// usedById reports whether this exact value is read, ignoring its name.
func (identifiers *liveIdentifiers) usedById(id static_single_assignment.IdentifierId) bool {
	return identifiers.identifiers[id]
}
