//go:build !unix

package intermediate_representation_test

// differentialPeakResident is unknown where getrusage isn't, as on Windows; the corpus runs measure on unix.
func differentialPeakResident() string {
	return "unknown"
}
