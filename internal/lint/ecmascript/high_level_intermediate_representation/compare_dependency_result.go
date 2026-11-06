package high_level_intermediate_representation

// CompareDependencyResult is how an inferred dependency relates to a written one.
//
// The constant order is upstream's and is load-bearing: `MergeCompareDependencyResults` takes the
// maximum, so a dependency compared against several source entries reports the most specific
// disagreement rather than the first one found.
type CompareDependencyResult uint8

func (result CompareDependencyResult) String() string {
	switch result {
	case CompareDependencyOk:
		return "ok"
	case CompareDependencyRootDifference:
		return "the inferred and source dependencies name different values"
	case CompareDependencyPathDifference:
		return "the inferred dependency reads a different path than the source declared"
	case CompareDependencySubpath:
		return "the source dependency is a prefix of the inferred one"
	case CompareDependencyRefAccessDifference:
		return "a partial match through a ref access, which proves nothing"
	default:
		return "<unknown compare-dependency result>"
	}
}
