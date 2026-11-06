// A file named utils.go, an import named utils and a variable named util are none of them a folder or a package
// clause, so none is judged.
package mention

import utils "strings"

// util is a variable, not a package.
var util = utils.ToUpper

// Shout uses the variable.
func Shout(text string) string {
	return util(text)
}
