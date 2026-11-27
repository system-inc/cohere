package micromark

// eventFixtures are the inputs that belong to no one construct. Each construct's port keeps its own list
// beside it, including the inputs where the construct must fail to match.
//
// The lists were chosen for comparing the port's events with the fork's micromark, event for event, which
// is why each one's comment says which constructs it stays clear of and why. No test compares them with
// upstream now (#5wj23dd); TestMemoryReuseMatchesFreshParses parses every one of them.
var eventFixtures = []string{
	"",
	"a",
	"a\nb",
	"a\n\nb",
	"  a  \n b",
	"***",
	"- - -",
	"_ _ _ _",
	"**",
	"a\\*b\\",
	"a\\\nb",
	"a  \nb",
	"a\t\nb",
	"a \n",
}
