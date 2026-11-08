package parity

import (
	"fmt"
	"regexp"
	renamed "regexp"
)

// compiler has methods named as regexp's functions are, which the check never takes for them.
type compiler struct{}

func (compiler) MustCompile(pattern string) string { return pattern }
func (compiler) Compile(pattern string) string     { return pattern }

// Compile is a function of this package named as regexp's is.
func Compile(pattern string) string { return pattern }

const escapedDot = "\\."

var compileLater = regexp.MustCompile

// packageLevel is a call in a package's variable initializer, which both walks reach.
var packageLevel = regexp.MustCompile("\\d+") // found

func found() {
	regexp.MustCompile("\\.")                           // found
	regexp.Compile("\\.")                               // found
	regexp.MustCompile("\\")                            // found, a lone escaped backslash
	regexp.MustCompile("a\\")                           // found, ending in an escaped backslash
	regexp.MustCompile("\\\\")                          // found, two escaped backslashes
	regexp.MustCompile("\\\\\\d")                       // found, three
	regexp.MustCompile("é\\d")                          // found, a multibyte rune before
	regexp.MustCompile("\\dé")                          // found, a multibyte rune after
	regexp.MustCompile(("\\."))                         // found at the literal inside the parentheses
	(regexp.MustCompile)("\\.")                         // found through the function's parentheses
	(regexp.Compile)(("\\w"))                           // found through both
	renamed.MustCompile("\\s")                          // found through a renamed import, named regexp.MustCompile
	renamed.Compile("\\s")                              // found through a renamed import, named regexp.Compile
	regexp.MustCompile("\\." /* comment */)             // found beside a comment
	fmt.Println(regexp.MustCompile("\\b"))              // found as another call's argument
	defer regexp.MustCompile("\\d")                     // found in a defer
	go regexp.MustCompile("\\d")                        // found in a go statement
	_ = func() any { return regexp.MustCompile("\\d") } // found in a function literal
}

func nearMisses(value string) {
	regexp.MustCompile(`\\.`)         // already raw
	regexp.MustCompile("abc")         // no backslash
	regexp.MustCompile("")            // empty
	regexp.MustCompile("\n")          // an escape, and no escaped backslash
	regexp.MustCompile("\\.\n")       // an escaped backslash beside another escape
	regexp.MustCompile("\\\n")        // an escaped backslash, then a newline's escape
	regexp.MustCompile("\\.\"")       // an escaped quote
	regexp.MustCompile("\\.\t")       // an escaped tab
	regexp.MustCompile("\\.\x41")     // a hexadecimal escape
	regexp.MustCompile("\\.\u00e9")   // a unicode escape
	regexp.MustCompile("\\.\101")     // an octal escape
	regexp.MustCompile("\\.`")        // a backtick, which a raw string can't hold
	regexp.MustCompile(escapedDot)    // a constant, no literal
	regexp.MustCompile("\\." + "a")   // a concatenation
	regexp.MustCompile(string("\\.")) // a conversion
	regexp.MustCompile(value)         // a variable
	regexp.MustCompilePOSIX("\\.")    // another function of regexp
	regexp.CompilePOSIX("\\.")        // another function of regexp
	regexp.MatchString("\\.", value)  // two arguments, and another function
	regexp.QuoteMeta("\\.")           // another function of regexp
	compileLater("\\.")               // a variable holding the function
	compile := regexp.Compile
	compile("\\.")                              // a local holding the function
	compiler{}.MustCompile("\\.")               // a method named alike
	compiler{}.Compile("\\.")                   // a method named alike
	Compile("\\.")                              // this package's function named alike
	regexp.MustCompile(regexp.QuoteMeta("\\.")) // a call, whose own literal is QuoteMeta's
	_ = packageLevel
}
