package load

import (
	"slices"
	"strings"
)

// flagsNoVerdictReads are the go flags a run's verdict cannot depend on, which VerdictFlags leaves out of every key a
// run keeps: -p bounds how many packages go builds at once, and -v, -x and -work only change what go prints or keeps of
// its work, while every go command runs as it would without them. The list errs narrow on purpose: a flag left in the
// key costs a miss when it changes, a flag left out wrongly replays a verdict another build would not give, so a flag
// joins it only when neither the load's success nor anything a rule reads can see it. That keeps -n in the key, which
// makes go print its commands without running them, so a load that needs cgo's output gets none, and -buildvcs, whose
// true can refuse a main package in a tree whose version control go cannot read. cohere-dev's pool sets -p to each
// token's share, so with it in the findings cache's key a run under another share missed every package, and in the kept
// listing's every listing.
var flagsNoVerdictReads = []string{"p", "v", "x", "work"}

// VerdictFlags is goFlags, `GOFLAGS` as go env reports it, without the flags in flagsNoVerdictReads, every other
// flag kept as written and in its order. go reads `GOFLAGS` as fields split on white space, each `-name` or
// `-name=value`, one dash or two.
func VerdictFlags(goFlags string) string {
	kept := []string{}
	for _, field := range strings.Fields(goFlags) {
		name, _, _ := strings.Cut(strings.TrimLeft(field, "-"), "=")
		if !slices.Contains(flagsNoVerdictReads, name) {
			kept = append(kept, field)
		}
	}
	return strings.Join(kept, " ")
}
