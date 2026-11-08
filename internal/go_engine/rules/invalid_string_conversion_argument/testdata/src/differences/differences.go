// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns every
// argument, so each argument's mark has only the one argument expression there is to sit on, and a strconv function
// held in a variable, whose callee is known only once the variable is lifted, or not at all when two functions meet.
// The port and the original find the same here, so the fixture records no difference.
package differences

import "strconv"

func textAndSize() (string, int) {
	return "", 16
}

func valueFormatPrecisionAndSize() (float64, byte, int, int) {
	return 0, 'j', -1, 16
}

func textBaseAndSize() (string, int, int) {
	return "", 1, 65
}

func statements(text string, number int64) {
	defer strconv.ParseFloat(text, 16)
	go strconv.FormatInt(number, 1)
	defer func() {
		strconv.ParseInt(text, 1, 65)
	}()
}

func multipleValues() {
	strconv.ParseFloat(textAndSize())
	strconv.FormatFloat(valueFormatPrecisionAndSize())
	strconv.ParseInt(textBaseAndSize())
}

func lifted(condition bool, number int64) {
	format := strconv.FormatInt
	if condition {
		format = strconv.FormatInt
	}
	format(number, 1)
	choose := strconv.FormatInt
	if condition {
		choose = func(int64, int) string { return "" }
	}
	choose(number, 1)
}
