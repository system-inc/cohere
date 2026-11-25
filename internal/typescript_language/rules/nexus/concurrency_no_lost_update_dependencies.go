package nexus

// concurrencyNoLostUpdateDependencies records what an evaluated value carries of the target's prior
// value: whether it was read before a suspension yet (stale) or not (fresh), and whether it is the
// value itself (verbatim) or something computed from it (modified).
type concurrencyNoLostUpdateDependencies uint8

// staled is what a suspension does to a value read before it.
func (dependencies concurrencyNoLostUpdateDependencies) staled() concurrencyNoLostUpdateDependencies {
	result := dependencies &^ (concurrencyNoLostUpdateFreshVerbatim | concurrencyNoLostUpdateFreshModified)
	if dependencies&concurrencyNoLostUpdateFreshVerbatim != 0 {
		result |= concurrencyNoLostUpdateStaleVerbatim
	}
	if dependencies&concurrencyNoLostUpdateFreshModified != 0 {
		result |= concurrencyNoLostUpdateStaleModified
	}
	return result
}

// modified is what any operation on a value does to it.
func (dependencies concurrencyNoLostUpdateDependencies) modified() concurrencyNoLostUpdateDependencies {
	result := dependencies &^ (concurrencyNoLostUpdateFreshVerbatim | concurrencyNoLostUpdateStaleVerbatim)
	if dependencies&concurrencyNoLostUpdateFreshVerbatim != 0 {
		result |= concurrencyNoLostUpdateFreshModified
	}
	if dependencies&concurrencyNoLostUpdateStaleVerbatim != 0 {
		result |= concurrencyNoLostUpdateStaleModified
	}
	return result
}
