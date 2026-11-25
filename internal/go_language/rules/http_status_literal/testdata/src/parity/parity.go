// Package parity holds every call and argument `ST1013` reads, each found and each near miss, under staticcheck's
// default list of codes left alone, for the original and the port to judge alike.
package parity

import (
	"net/http"
	web "net/http"
)

// statusCode is a named constant, which is not a literal.
const statusCode = 503

// responseAndCode is three values, passed to http.Error whole.
func responseAndCode() (http.ResponseWriter, string, int) { return nil, "", 0 }

// codeOf is a call, which is not a literal.
func codeOf() int { return 503 }

// handlers takes the code at each function's own argument.
func handlers(writer http.ResponseWriter, request *http.Request) {
	// Each function at its code's argument.
	http.Error(writer, "", 503)
	http.Redirect(writer, request, "", 301)
	_ = http.StatusText(418)
	_ = http.RedirectHandler("", 307)

	// A literal in another argument is not the code.
	_ = http.RedirectHandler("", 300+7)
	http.Error(writer, "503", 0)

	// The literal's spellings: hexadecimal, octal, underscored, signed and parenthesized.
	_ = http.StatusText(0x1F7)
	_ = http.StatusText(0o777)
	_ = http.StatusText(0627)
	_ = http.StatusText(5_03)
	_ = http.StatusText(+503)
	_ = http.StatusText(-503)
	_ = http.StatusText((503))
	_ = (http.StatusText)(503)
	_ = http.StatusText(-(-503))

	// The codes left alone by default, and their neighbours.
	_ = http.StatusText(200)
	_ = http.StatusText(201)
	_ = http.StatusText(400)
	_ = http.StatusText(404)
	_ = http.StatusText(500)
	_ = http.StatusText(+404)

	// Codes net/http names that the original's table lacks, and codes nobody names.
	_ = http.StatusText(103)
	_ = http.StatusText(421)
	_ = http.StatusText(425)
	_ = http.StatusText(600)
	_ = http.StatusText(0)

	// What is not an integer literal: a constant, a conversion, a character, an expression and a call.
	_ = http.StatusText(statusCode)
	_ = http.StatusText(http.StatusAccepted)
	_ = http.StatusText(int(503))
	_ = http.StatusText('d')
	_ = http.StatusText(500 + 3)
	_ = http.StatusText(codeOf())
	http.Error(responseAndCode())

	// net/http imported under another name, whose fix still writes http.
	web.Error(writer, "", 502)

	// A function value is not the function.
	statusText := http.StatusText
	_ = statusText(503)
	redirect := http.Redirect
	redirect(writer, request, "", 301)

	// A function of the same name elsewhere, and a method of net/http, are not the function.
	_ = StatusText(503)
	http.NotFound(writer, request)
	_ = request.Header.Get("503")
	header := http.Header{}
	header.Set("Retry-After", "503")
}

// StatusText is this package's own, which the check leaves.
func StatusText(code int) string { return "" }
