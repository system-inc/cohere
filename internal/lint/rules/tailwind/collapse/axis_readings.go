package tailwind

// AxisReadings is one modifier axis's worth of a root's readings.
//
// The three axes hold the same shape, so they are one type used three times rather than nine fields
// on Descriptor.
type AxisReadings struct {
	// ByType maps a data type to its reading. A type in the root's TypeList and absent here reads
	// like Fallback, which is the fourth refinement above: presence in the list means the type
	// discriminates on some axis, not on this one.
	ByType map[DataType]Reading
	// ByNamespace maps a theme namespace to the reading a key in it produces. Keyed on `--color`,
	// `--font-weight` and so on, plus the two pseudo-namespaces below.
	//
	// Unlike ByType, entries here are kept even when they equal Fallback, and the difference is not
	// a matter of taste. A miss in ByType ends the lookup at the fallback, so an elided entry and an
	// absent one are the same answer. A miss in ByNamespace does not end anything: it continues the
	// bare-value precedence to inference and then to `@none`, so eliding an entry does not return
	// the fallback, it hands the value to a later step that answers differently.
	//
	// This was measured rather than reasoned about. Eliding it looked structural and correct, and it
	// silently changed 647 namespace buckets across 303 roots: `font-bold` is a `--font-weight` key
	// whose reading happens to equal `font`'s fallback, so eliding it dropped the value through to
	// inference, which types `bold` as a `family-name` and reads `[284]#1` where the engine reads
	// `[288]#2`. That is refinement 2 failing in exactly the way refinement 2 exists to prevent, and
	// it was invisible to every test that did not compare against the engine.
	ByNamespace map[string]Reading
	// Fallback is the reading for a value this axis has nothing more specific for. ByType omits
	// entries equal to it; ByNamespace does not, for the reason above.
	Fallback Reading
	// Empty is the reading for the root written with no value at all, such as `border`. Absent when
	// the root rejects that form, which is why it is a pointer: a root that reads `[]#0` for the
	// empty form and a root that refuses the empty form are different facts, and a zero Reading
	// cannot express both.
	Empty *Reading
}

// readingForType is the third and fourth refinements in one line: a type absent from this axis reads
// like the axis fallback, because presence in a root's TypeList means the type discriminates on
// *some* axis rather than on this one, and the generator drops every bucket equal to the fallback.
func (axis AxisReadings) readingForType(dataType DataType) Reading {
	if reading, found := axis.ByType[dataType]; found {
		return reading
	}
	return axis.Fallback
}
