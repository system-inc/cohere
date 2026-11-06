package compose

// ScalarValue is a Scalar's resolved value, one of the JavaScript values upstream's tags resolve to: a
// ScalarBoolean, ScalarNumber, ScalarString, *MergeKey, Date or Binary, with nil for null. The unexported
// method seals the set: it only declares that a variant belongs, nothing calls it, and a type switch
// reads which variant a value is.
type ScalarValue interface {
	scalarValue()
}
