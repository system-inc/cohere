// Package stopping holds, for the facts' reach (#z9fze0t), a callee the build lints with no record of the stopping
// calls, a kind that keeps the unrecorded rule whole: no body asks helper's, which stopVia's judgment walks into, and
// helper selects a method of a testing.T, whose layout is each build's own, so the reach lints it. stopVia names only
// helper, so the reach leaves it standing, and only helper's missing record takes it out. The host answers the stopping
// calls and purity about the package, and no other kind.
package stopping

import "testing"

// current is the test the chain reads, kept out of every signature.
var current *testing.T

// helper calls FailNow on what each platform declares.
func helper() {
	platformTest(current).FailNow()
}

// stopVia names only helper.
func stopVia() {
	helper()
}

// StoppingUser starts stopVia in a goroutine, a finding where stopVia stops the test.
func StoppingUser() {
	go stopVia()
}
