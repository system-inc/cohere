package gate

// Caller calls copy, which names the package's own copy in the build that declares one and the builtin in the other.
func Caller(values []int) int { return copy(values, values[1:]) }

// Plain names no copy.
func Plain(values []int) int { return len(values) }
