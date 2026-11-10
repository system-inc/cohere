package testing_no_machine_path

import (
	"strconv"
	"testing"
)

// mayNameHomeDirectory sets a literal aside without unquoting it only when unquoting could not make it name a home
// directory: every literal here whose unquoted value homeDirectory matches is one it keeps, escapes and a raw string's
// carriage return included, and the literals naming none and holding no escape are ones it sets aside, which is
// what makes it cheaper than unquoting every literal.
func TestMayNameHomeDirectoryKeepsEveryLiteralThatNamesOne(test *testing.T) {
	test.Parallel()
	for _, literal := range []struct {
		source   string
		setAside bool
	}{
		{source: `"/Users/someone/x"`},
		{source: "`/home/someone/x`"},
		{source: `"\x2fUsers\x2fsomeone\x2fx"`},
		{source: `"\057home\057someone\057x"`},
		{source: `"\u002fUsers/someone/"`},
		{source: "`/Us\rers/someone/x`"},
		{source: `"/Users/"`},
		{source: `"/tmp/scratch/file"`, setAside: true},
		{source: "`plain text`", setAside: true},
		{source: `"/AllUsers"`, setAside: true},
		{source: `""`, setAside: true},
	} {
		value, unquoteError := strconv.Unquote(literal.source)
		if unquoteError != nil {
			test.Fatalf("the case %q does not unquote: %v", literal.source, unquoteError)
		}
		kept := mayNameHomeDirectory(literal.source)
		if homeDirectory.MatchString(value) && !kept {
			test.Errorf("%q names a home directory once unquoted, %q, and was set aside unread", literal.source, value)
		}
		if literal.setAside && kept {
			test.Errorf("%q names no home directory and holds no escape, and was kept for unquoting", literal.source)
		}
	}
}
