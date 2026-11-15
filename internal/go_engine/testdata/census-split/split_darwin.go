package split

// Split is darwin's, other code than windows' though its path is the same.
func Split(dst []byte) []byte { return append(dst, 'd') }
