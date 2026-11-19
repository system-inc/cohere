package p

// Shared is in a file both builds compile and reads nothing that differs: independent.
func Shared() int { return 2 }
