// Package first declares its callers in its first file and what they call in its second, beside an unrelated
// function. staticcheck's push walks the callers first, meets the instance and the wrapper before it has judged what
// they stand for, and holds both callers not pure. Package second is this package with the files the other way round
// and without Unrelated, and the push holds both pure there. Each caller's purity is the same in both (#zva1zrc).
package first

// InstanceCaller calls an instance of the package's own pure generic function.
func InstanceCaller() int {
	return Zero[string](1)
}

// WrapperCaller calls the package's own pure method through a method expression, which stands for it through a
// wrapper.
func WrapperCaller(value int) int {
	return Count.Double(Count(value))
}

// Unrelated is a function nothing else calls, which neither caller's purity may turn on.
func Unrelated(value int) int {
	return value - 1
}
