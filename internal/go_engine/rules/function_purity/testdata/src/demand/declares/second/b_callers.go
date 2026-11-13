// Package second is package first with its two files the other way round and without first's unrelated function.
// staticcheck's push judges what the callers stand for first here, and holds both callers pure (#zva1zrc).
package second

// InstanceCaller calls an instance of the package's own pure generic function.
func InstanceCaller() int {
	return Zero[string](1)
}

// WrapperCaller calls the package's own pure method through a method expression, which stands for it through a
// wrapper.
func WrapperCaller(value int) int {
	return Count.Double(Count(value))
}
