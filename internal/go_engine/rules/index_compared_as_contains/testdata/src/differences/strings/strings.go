// Package strings is a package of another path that shares the standard library's name, whose functions the
// original judges by the name they are called through.
package strings

func Index(s string, substring string) int { return len(s) - len(substring) }

func IndexAny(s string, characters string) int { return len(s) - len(characters) }

func IndexRune(s string, r rune) int { return len(s) - int(r) }

func LastIndex(s string, substring string) int { return len(s) - len(substring) }
