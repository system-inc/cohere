// Package parity holds the shapes staticcheck's representation builds a pattern or a call in more than one way, and
// the regexp package's errors of each kind, both the patterns that compile and those that do not.
package parity

import (
	"io"
	"regexp"
	"strings"
)

type pattern string

const typed pattern = `a(`

const (
	open    = "("
	grouped = "(" + "a" + ")"
)

var compiled = regexp.MustCompile(`\d+`)

var (
	broken   = regexp.MustCompile(open)
	repeated = regexp.MustCompile(`x{1001}`)
)

func init() {
	regexp.MustCompile(`a**`)
}

func errorsOfEachKind() {
	regexp.MustCompile(`\8`)
	regexp.MustCompile(`[z-a]`)
	regexp.MustCompile(`(?P<>a)`)
	regexp.MustCompile(`\pX`)
	regexp.MustCompile(`a)`)
	regexp.MustCompile(`(?i`)
	regexp.MustCompile(`[[:alpha:]`)
	regexp.MustCompile("\xff")
}

func patternsThatCompile() {
	regexp.MustCompile(grouped)
	regexp.MustCompile(`^[a-z]+\[[0-9]+\]$`)
	regexp.MustCompile("")
	regexp.MatchString(`(?P<name>a)`, "a")
	regexp.MustCompilePOSIX(`a`)
}

func constantShapes() {
	regexp.MustCompile(string(typed))
	regexp.MustCompile("a" + open)
	regexp.MustCompile((`(`))
	local := `[`
	regexp.MustCompile(local)
	const declared = `*`
	regexp.Compile(declared)
}

func valuesNotConstant(argument string, condition bool) {
	regexp.MustCompile(argument)
	regexp.MustCompile(argument + "(")
	regexp.MustCompile(strings.Repeat("(", 2))
	differing := "("
	if condition {
		differing = "[" // Two different constants meet in a phi, which is no one constant.
	}
	regexp.MustCompile(differing)
	agreeing := "("
	if condition {
		agreeing = "("
	}
	regexp.MustCompile(agreeing)
}

func callShapes(reader io.RuneReader) {
	defer regexp.MatchString("(", "")
	go regexp.Match("[", nil)
	func() {
		regexp.MatchReader(`)`, reader)
	}()
	compile := regexp.MustCompile
	compile(`(`)
	regexp.MustCompile(
		`(`,
	)
	if matched, matchError := regexp.MatchString(`\`, "a"); matched || matchError != nil {
		return
	}
	_ = regexp.MustCompile(`(`).String()
	_ = compiled.MatchString(`(`)
	_ = broken
	_ = repeated
}

func generic[T any](value T) T {
	regexp.MustCompile(`(`)
	return value
}

func instantiated() {
	generic(1)
}
