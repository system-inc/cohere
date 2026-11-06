package high_level_intermediate_representation

// MutationSiteKind is the instruction shape that makes a site a mutation candidate.
type MutationSiteKind uint8

func (kind MutationSiteKind) String() string {
	switch kind {
	case MutationSitePropertyStore:
		return "property-store"
	case MutationSiteComputedStore:
		return "computed-store"
	case MutationSiteStoreContext:
		return "store-context"
	case MutationSiteCall:
		return "call"
	default:
		return "<unknown>"
	}
}
