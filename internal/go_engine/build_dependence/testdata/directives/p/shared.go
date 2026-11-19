package p

// Shared is in a file both builds compile and reads nothing that differs, yet windows seeds it: a file only the host
// compiles holds a line directive, which could name any file of p, so every unit of p is a seed.
func Shared() int { return 2 }
