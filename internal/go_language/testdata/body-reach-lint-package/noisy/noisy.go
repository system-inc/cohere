// Package noisy writes a field it never reads, which unusedwrite reports in every build, and nothing any build changes
// reaches, so a build restoring from the host's run would leave it untouched.
package noisy

// Write writes a field no one reads.
func Write() {
	var x struct{ field int }
	x.field = 1
}
