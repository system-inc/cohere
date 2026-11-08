//go:build linux && amd64

// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none. The original matches its patterns against every node, a comparison's parentheses
// as well as the comparison, and judges a constraint by trying every combination of its other tags, so these
// comparisons are parenthesized at several levels and judged against constraints with other tags in several places,
// where a walk or a judgment of the port's own would show as a finding one side makes and the other does not. The
// package's files are constrained to linux and amd64, with use.go building everywhere, so the package loads for
// every target the test reads.
package differences

import "runtime"

// Found by both, twice each: the parentheses and the comparison inside, with parentheses on each side too.
func parentheses() {
	use(((runtime.GOOS) == ("windows")))
	use((runtime.GOARCH != (`arm64`)))
}

// Found by both: both targets at once, each judged against its own half of the constraint.
func both() {
	use(runtime.GOOS == "windows" || runtime.GOARCH == "386")
}

// Found by neither: values the constraint allows.
func allowed() {
	use(runtime.GOOS == "android", runtime.GOARCH == "amd64")
}
