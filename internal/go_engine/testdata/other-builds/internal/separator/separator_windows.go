// Package separator is what joins two parts of a path on the platform built for.
package separator

// Separator joins two parts of a path.
const Separator = `\`

// Join joins two parts of a path, the first alone when the second is empty. Its else after a return is a finding
// only windows's build can see.
func Join(first string, second string) string {
	if second == "" {
		return first
	} else {
		return first + Separator + second
	}
}
