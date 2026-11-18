package parity

// IntSlice is a slice type of the code's own, named as package sort's helper is.
type IntSlice []int

// Sort is a function of the code's own, named as package sort's is.
func Sort(values IntSlice) {}

// A file that imports no package sort calls no sort.Sort, whatever its calls are named, so neither finds this.
func localLookalikes(values []int) {
	Sort(IntSlice(values))
	func() {
		Sort(IntSlice(nil))
	}()
}
