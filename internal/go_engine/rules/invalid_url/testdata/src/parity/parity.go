// Package parity holds the shapes staticcheck's representation builds a URL or a call in more than one way, and URLs
// of each kind url.Parse refuses, beside URLs it reads.
package parity

import "net/url"

type address string

const typed address = "http://[::1"

const (
	broken = "%zz"
	joined = "https://" + "golang.org"
)

var parsed, parseError = url.Parse("http://a b.com/")

func init() {
	url.Parse("\x7f")
}

func urlsOfEachKind() {
	url.Parse("https://golang.org/doc?query=1#fragment")
	url.Parse("mailto:someone@example.com")
	url.Parse("/relative/path")
	url.Parse("")
	url.Parse("http://[fe80::1%en0]:8080/")
	url.Parse("http://host:port")
	url.Parse("http://%41:8080/")
	url.Parse("1http://golang.org")
	url.Parse("http://golang.org/%")
	url.Parse("http://[::1]a")
	url.Parse("\n")
	url.ParseRequestURI(":")
}

func constantShapes() {
	url.Parse(string(typed))
	url.Parse(broken)
	url.Parse(joined)
	url.Parse((":"))
	local := ":"
	url.Parse(local)
	const declared = "%"
	url.Parse(declared)
	url.Parse(":" + joined)
}

func valuesNotConstant(argument string, condition bool) {
	url.Parse(argument)
	url.Parse(argument + ":")
	differing := ":"
	if condition {
		differing = "%"
	}
	url.Parse(differing)
}

func callShapes() {
	defer url.Parse(":")
	go url.Parse(":")
	func() {
		url.Parse(":")
	}()
	parse := url.Parse
	parse(":")
	url.Parse(
		":",
	)
	if _, err := url.Parse(":"); err != nil {
		return
	}
	_ = parsed
	_ = parseError
}

func generic[T any](value T) T {
	url.Parse(":")
	return value
}
