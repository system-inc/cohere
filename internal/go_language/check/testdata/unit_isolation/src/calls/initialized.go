package calls

// limit is declared above its readers and set by init, below them, so a rule that judged a variable by where another
// declaration's body assigns it, rather than where it is declared, would read init's body from Limit.
var limit int

// Limit reads limit, declared above.
func Limit() int {
	return limit
}

// Limited reads limit too.
func Limited() bool {
	return limit > 0
}

func init() {
	limit = 3
}
