// A file that is not Go: every //go:debug is only valid in Go source files.
//go:debug panicnil=1
//go:debug	panicnil=1
//go:debug panicnil=1
//go:unknownverbx

/* one line */ /* and another */

/* a block comment
//go:debug inside it is skipped
   and closed */ /* opened and
closed */

  // An indented comment line is no comment line here, and it ends the reading, so what follows is clean.
//go:debug panicnil=1
