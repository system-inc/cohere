// Package differences holds the comparisons where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: an address taken through a dereference or of a converted value's variable,
// comparisons nested in others and negated, and a switch on an address with a nil case, which is no binary expression.
package differences

type celsius float64

func plausible(pointer *int, temperature float64, flag bool) {
	_ = &*pointer == nil
	_ = !(&temperature == nil)
	_ = flag == (&temperature != nil)
	_ = (*celsius)(&temperature) == nil
	_ = &*(*celsius)(&temperature) != nil
	switch &temperature {
	case nil:
	}
	switch {
	case &flag == nil:
	}
}
