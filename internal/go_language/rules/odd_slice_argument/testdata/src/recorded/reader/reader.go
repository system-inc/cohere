// Package reader asks one mark of package subject, Back's slice's, by passing it a slice of one.
package reader

import "recorded/subject"

// odd passes subject.Back a slice of one.
func odd() {
	subject.Back([]string{"one"})
}
