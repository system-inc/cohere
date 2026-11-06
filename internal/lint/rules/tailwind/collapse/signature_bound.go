package tailwind

// SignatureBound is the properties one class's signature must and might declare.
type SignatureBound struct {
	// Certain are the properties the signature declares whatever this port is unsure of.
	Certain map[string]bool
	// Maybe are the properties it might also declare.
	Maybe map[string]bool
	// Unbounded is a class this port knows nothing about.
	Unbounded bool
}

// Admits reports whether a signature declaring exactly these properties is within the bound: the
// possible-match relation, which answers true whenever it cannot answer false.
func (bound SignatureBound) Admits(properties map[string]bool) bool {
	if bound.Unbounded {
		return true
	}
	for property := range bound.Certain {
		if !properties[property] {
			return false
		}
	}
	for property := range properties {
		if !bound.Certain[property] && !bound.Maybe[property] {
			return false
		}
	}
	return true
}
