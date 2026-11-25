// Package reader reads logger's Line, whose own line every build declares alike.
package reader

import "example.com/walkrejudge/logger"

// Twice is Line twice.
func Twice() string { return logger.Line() + logger.Line() }
