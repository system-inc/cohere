// Package walked holds ifs and a label across two files, nested at several depths, for the syntax dispatcher to walk.
package walked

func choose(value int) int {
	if value > 0 {
		if value > 10 {
			return 2
		}
		return 1
	}
outer:
	for {
		break outer
	}
	return 0
}
