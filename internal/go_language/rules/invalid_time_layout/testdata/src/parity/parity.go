// Package parity holds the shapes staticcheck's representation builds a layout or a call in more than one way, and
// layouts of each kind time.Parse reads, both those written in the reference time and those that are not.
package parity

import "time"

type layout string

const typed layout = "2006-13-02"

const (
	broken = "01/02/2007"
	joined = "2006" + "-01-02"
)

var parsed, parseError = time.Parse("15:04:61", "")

func init() {
	time.Parse("Jan 32", "")
}

func layoutsOfEachKind() {
	time.Parse("2006-01-02T15:04:05Z07:00", "")
	time.Parse("Mon Jan _2 15:04:05 2006", "")
	time.Parse("2006-01-02 15:04:05.000000000 -0700 MST", "")
	time.Parse("02 Jan 06 15:04 MST", "")
	time.Parse("2006-01-02T15:04:05Z", "")
	time.Parse("___2", "")
	time.Parse("", "")
	time.Parse("hello", "")
	time.Parse("2006-02-30", "")
	time.Parse("15:60", "")
	time.Parse("Mon Jam 2", "")
	time.Parse("2006-01-02T25:00", "")
	time.Parse(time.RFC1123Z, "")
	time.Parse(time.DateTime, "")
	time.ParseInLocation("01/02/2007", "", time.UTC)
}

func constantShapes() {
	time.Parse(string(typed), "")
	time.Parse(broken, "")
	time.Parse(joined, "")
	time.Parse(("2006-13-01"), "")
	local := "2006-00-01"
	time.Parse(local, "")
	const declared = "01/32/2006"
	time.Parse(declared, "")
}

func valuesNotConstant(argument string, condition bool) {
	time.Parse(argument, "")
	time.Parse(argument+"-13", "")
	differing := "2006-13-01"
	if condition {
		differing = "2006-01-32"
	}
	time.Parse(differing, "")
}

func callShapes() {
	defer time.Parse("2006-13-01", "")
	go time.Parse("2006-13-01", "")
	func() {
		time.Parse("2006-13-01", "")
	}()
	parse := time.Parse
	parse("2006-13-01", "")
	time.Parse(
		"2006-13-01",
		"",
	)
	if _, err := time.Parse("2006-13-01", ""); err != nil {
		return
	}
	_ = parsed
	_ = parseError
}

func generic[T any](value T) T {
	time.Parse("2006-13-01", "")
	return value
}
