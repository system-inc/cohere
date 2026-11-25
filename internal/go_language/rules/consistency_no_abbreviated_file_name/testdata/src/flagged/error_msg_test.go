// want `^File name "error_msg_test\.go" abbreviates "msg"\. Spell it out as "message"\.`

// The name's last word, once go test's _test is set aside, read as an identifier's suffix.
package flagged

import "testing"

func TestPrint(test *testing.T) {
	test.Parallel()
	if Print("text") != "text" {
		test.Fatal("Print changed its text")
	}
}
