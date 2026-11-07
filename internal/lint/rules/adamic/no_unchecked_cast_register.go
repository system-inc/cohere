package adamic

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers adamic/no-unchecked-cast. Which casts Adamic checks is Adamic's decision (#bw3xg7c), not a
// project's; the one option says whether a compiler emits those checks at all, and a project on tsc sets it
// false (#mdccedj). The decoder fills the default, so a bare severity keeps Adamic's meaning.
func init() {
	rule.Register(rule.Registration[NoUncheckedCastOptions]{
		Rule:   NoUncheckedCast,
		Decode: DecodeNoUncheckedCastOptions,
	})
}
