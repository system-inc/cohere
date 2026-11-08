// Package strings is spelled as the standard library's package is and is another package, at the import path
// lookalike, with every function S1017 asks about, so a call of one is spelled as a call of the real one.
package strings

func HasPrefix(text string, prefix string) bool { return false }

func HasSuffix(text string, suffix string) bool { return false }

func Contains(text string, part string) bool { return false }

func TrimPrefix(text string, prefix string) string { return text }

func TrimSuffix(text string, suffix string) string { return text }

func Replace(text string, old string, replacement string, count int) string { return text }
