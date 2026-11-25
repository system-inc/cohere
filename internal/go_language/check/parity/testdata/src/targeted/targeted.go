// Package targeted holds a file for 386 alone and one for every other target: the first calls shared, which both
// sides find, under a note; the second calls lost, which only the original finds. Loaded for 386 the two agree and
// meet the note, and loaded for any other target, as the machine's own, they differ and the fixture holds no note.
package targeted

func shared() {}

func lost() {}
