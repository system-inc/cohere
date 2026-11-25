// Package shadow declares its own nil, so no nil here is the universe's, and neither side reports.
package shadow

var nil *int

func packageNil(i interface{}) {
	if _, ok := i.(string); ok && i != nil {
	}
	if i != nil {
		if _, ok := i.(string); ok {
		}
	}
}
