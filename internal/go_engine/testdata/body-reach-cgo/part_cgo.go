package gate

import "C"

// Other is the host's here, where cgo is on and translates this file, whose translation imports unsafe, syscall and
// runtime/cgo; windows, built without cgo, compiles part_nocgo.go in its place.
func Other() int { return 0 }
