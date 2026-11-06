package allowed

// Lookalikes of a capitalized local value: lowerCamel locals, a type declared inside a function, which is UpperCamel
// wherever it stands, and package-level names, whose capital is Go's export.
var PackageLimit = 10

const PackageTimeout = 5

func biggest(limit int) (largest int) {
	type LocalShape struct{ Size int }
	maxSize := LocalShape{Size: PackageLimit}.Size
	const httpTimeout = PackageTimeout
	for index, value := range []int{maxSize, httpTimeout, limit} {
		largest = max(largest, index+value)
	}
	return largest
}

// Lowered, Type would be the keyword type, which no suggestion can name.
func keyword() int {
	Type := 1
	return Type
}
