package marker

// Windows says the build is windows's, a method only windows declares on Marker.
func (Marker) Windows() bool {
	return true
}
