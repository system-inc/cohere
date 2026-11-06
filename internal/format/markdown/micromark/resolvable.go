package micromark

// resolvable is upstream's resolveAllConstructs entry: a construct, or the initial construct, which also
// carries a resolveAll. Compared by identity.
type resolvable struct {
	construct  *Construct
	initialize *InitialConstruct
}

func (entry resolvable) resolveAll() *Resolver {
	if entry.construct != nil {
		return entry.construct.ResolveAll
	}
	return entry.initialize.ResolveAll
}
