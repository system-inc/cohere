package flagged

import "testing"

// The Test prefix is go test's grammar, and everything after it is ours.
func TestParse_line(test *testing.T) {} // want "`TestParse_line` has an underscore in it; spell it `TestParseLine`"

// The rest must still open with a capital, or go test would stop running it.
func Test_parse(test *testing.T) {} // want "`Test_parse` has an underscore in it; spell it `TestParse`"

// The rest alone is in screaming snake case, so the shouting message speaks for it.
func TestMAX_RETRIES(test *testing.T) {} // want "RETRIES` is in screaming snake case; spell it `TestMaxRetries`"

func BenchmarkParse_line(benchmark *testing.B) {} // want "`BenchmarkParse_line` has an underscore in it"

func FuzzParse_input(fuzz *testing.F) {} // want "`FuzzParse_input` has an underscore in it; spell it `FuzzParseInput`"

// A helper whose name only starts like a test is not one, so its whole name is judged.
func Testify_helper() {} // want "`Testify_helper` has an underscore in it; spell it `TestifyHelper`"

// A method is never an example, whatever its name.
type suite struct{}

func (current suite) Example_method() {} // want "`Example_method` has an underscore in it; spell it `ExampleMethod`"

// A segment in capitals alone shouts, though it follows the word.
func TestPARSE_LINE(test *testing.T) {} // want "`TestPARSE_LINE` is in screaming snake case; spell it `TestParseLine`"

func TestParse_LINE(test *testing.T) {} // want "`TestParse_LINE` has an underscore in it; spell it `TestParseLine`"

// An empty segment is no word.
func TestParse__Line(test *testing.T) {} // want "`TestParse__Line` has an underscore in it; spell it `TestParseLine`"

// A test's name with a signature go test does not call is no test's, so the whole name is ours.
func TestParse_Line() {} // want "`TestParse_Line` has an underscore in it; spell it `TestParseLine`"

func BenchmarkParse_Line(test *testing.T) {} // want "`BenchmarkParse_Line` has an underscore in it; spell it `BenchmarkParseLine`"

func ExampleParse_Line(value int) {} // want "`ExampleParse_Line` has an underscore in it; spell it `ExampleParseLine`"

// An example's lower-case segment is its suffix only last, and a segment in capitals names no method here.
func ExampleClient_send_Retries() {} // want "`ExampleClient_send_Retries` has an underscore in it; spell it `ExampleClientSendRetries`"

func ExampleClient_URL() {} // want "`ExampleClient_URL` has an underscore in it; spell it `ExampleClientUrl`"
