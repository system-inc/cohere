package caller

import "example.com/restorebodiless/intrinsic"

// doubled is twice Raw's value, so judging whether it is pure asks whether Raw is.
func doubled() int { return intrinsic.Raw(1) * 2 }

// Discard calls doubled and drops its result, so discarded_pure_call asks whether doubled is pure.
func Discard() { doubled() }
