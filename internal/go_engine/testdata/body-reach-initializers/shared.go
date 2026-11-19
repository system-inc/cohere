// Package initializers holds three variable initializers each build shares, every one a call staticcheck reports on
// its constant argument, for the body reach (#bed0mnk). Go lowers every package-level variable initializer into one
// synthetic function, in file order, so a platform file's initializer that never returns, sorted before this file,
// leaves these unreachable on its platform alone, and their findings with them. The test writes the platform files.
package initializers

import (
	"net/url"
	"regexp"
	"time"
)

// parsed is a URL that never parses.
var parsed, _ = url.Parse(":bad")

// pattern is an expression that never compiles.
var pattern = regexp.MustCompile("(")

// when is a time read by a layout that holds no month 13.
var when, _ = time.Parse("2006-13-01", "x")
