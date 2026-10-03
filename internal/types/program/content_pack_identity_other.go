//go:build !darwin && !linux

package program

// statIdentity reports nothing here, so the content pack serves nothing and every file is read from disk.
// A change time is what makes a pack entry safe to serve, and these platforms do not give one through the
// same call.
func statIdentity(path string) (fileIdentity, bool) {
	return fileIdentity{}, false
}
