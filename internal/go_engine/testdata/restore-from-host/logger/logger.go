// Package logger hands Logf a format and an argument that does not fit it.
package logger

import "example.com/restore/platform"

// Line is a line logged.
func Line() string { return platform.Logf("%d", "text") }
