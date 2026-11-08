// Package parity holds every branch of timeformat's judgment and the shapes most likely to differ between the
// original and the port, each beside a clean case, for the differential test. The original and the port must make the
// same findings here, at the same spans, with the same fixes. A literal whose escapes come before or inside the
// mistake is where the two part, and lives in the differences fixture instead.
package parity

import (
	"time"
	clock "time"
)

// A literal, interpreted and raw, as Parse's layout and Format's, alone and beside other text: each is a finding over
// the mistake's own bytes, with the fix.
func literals(moment time.Time) {
	_, _ = time.Parse("2006-02-01", "2021-01-01")
	_, _ = time.Parse(`2006-02-01`, "2021-01-01")
	_ = moment.Format("2006-02-01")
	_ = moment.Format(`2006-02-01`)
	_ = moment.Format("Mon 2006-02-01 15:04")
	_ = moment.Format(`Mon 2006-02-01 15:04`)
	// Text in more than one byte per character before the mistake: the value's bytes are the source's.
	_ = moment.Format("日付 2006-02-01")
	_ = moment.Format(`日付 2006-02-01`)
	// An escape after the mistake leaves the bytes before it where they were.
	_ = moment.Format("2006-02-01\t15:04")
	// Only the first mistake is reported, as strings.Index finds it.
	_ = moment.Format("2006-02-01 2006-02-01")
	_ = moment.Format("2006-02-012006-02-01")

	_, _ = time.Parse("2006-01-02", "2021-01-01")
	_ = moment.Format(`2006-01-02`)
	_ = moment.Format("2006-01-02 15:04")
	_ = moment.Format("2006-02")
	_ = moment.Format("02-01")
	_ = moment.Format("2006-02-0")
	_ = moment.Format("2006 02 01")
}

// layout is a typed package constant, and untypedLayout an untyped one, each reported at the name, with no fix.
const (
	layout        string = "2006-02-01"
	untypedLayout        = "2006-02-01"
	goodLayout           = "2006-01-02"
)

// text is an alias for string. A constant of it is recorded with the alias as its type, which the original does not
// look through, so neither side judges it, while its conversion to string is of type string and judged.
type text = string

const aliasedLayout text = "2006-02-01"

// Every constant that is not one literal is reported at its start, with no fix.
func constants(moment time.Time) {
	_ = moment.Format(layout)
	_ = moment.Format(untypedLayout)
	_ = moment.Format("2006-" + "02-01")
	_ = moment.Format("\t" + "2006-02-01")
	_ = moment.Format(("2006-02-01"))
	_ = moment.Format(string("2006-02-01"))
	_ = moment.Format(string(layout))
	const local = "Mon " + layout
	_ = moment.Format(local)
	_, _ = time.Parse(layout+" 15:04", "2021-01-01 00:00")
	_ = moment.Format(string(aliasedLayout))

	_ = moment.Format(goodLayout)
	_ = moment.Format(aliasedLayout)
}

// A layout the compiler does not know is never judged.
func variables(moment time.Time, layouts []string, byName map[string]string) {
	variable := "2006-02-01"
	_ = moment.Format(variable)
	_ = moment.Format(layouts[0])
	_ = moment.Format(byName["date"])
	_ = moment.Format(returned())
	_ = moment.Format(generic("2006-02-01"))
}

func returned() string {
	return "2006-02-01"
}

func generic[Layout ~string](layout Layout) string {
	return string(layout)
}

// holder embeds time.Time, so its Format is time.Time's, promoted.
type holder struct {
	time.Time
}

// aliased is an alias for time.Time, whose methods are time.Time's.
type aliased = time.Time

// defined is a new type over time.Time, which has none of its methods.
type defined time.Time

func (moment defined) Format(layout string) string {
	return time.Time(moment).Format(layout)
}

// Every way to reach time.Time's Format or time's Parse through a value, a pointer, an embedding, an alias, a renamed
// import, a parenthesized callee or a closure is judged.
func receivers(moment time.Time, pointer *time.Time, embedded holder, embeddedPointer *holder, other aliased) {
	_ = pointer.Format("2006-02-01")
	_ = (&moment).Format("2006-02-01")
	_ = embedded.Format("2006-02-01")
	_ = embeddedPointer.Format("2006-02-01")
	_ = embedded.Time.Format("2006-02-01")
	_ = other.Format("2006-02-01")
	_, _ = clock.Parse("2006-02-01", "2021-01-01")
	_ = (moment.Format)("2006-02-01")
	_, _ = (time.Parse)("2006-02-01", "2021-01-01")
	_ = time.Now().Format("2006-02-01")
	closure := func() string {
		return moment.Format("2006-02-01")
	}
	_ = closure()
	go func() {
		_ = moment.Format("2006-02-01")
	}()
	defer moment.Format("2006-02-01")
}

// formatter is any value with a Format method of time.Time's shape.
type formatter interface {
	Format(layout string) string
}

func formatOf[Value formatter](value Value) string {
	return value.Format("2006-02-01")
}

// Calls of anything else named Format or Parse, a method or function time.Time and time do not declare, or a function
// value, are never judged.
func others(moment time.Time, value defined, through formatter) {
	_ = value.Format("2006-02-01")
	_ = through.Format("2006-02-01")
	_ = formatOf(moment)
	_ = Parse("2006-02-01")
	_, _ = time.ParseInLocation("2006-02-01", "2021-01-01", time.UTC)
	_ = moment.AppendFormat(nil, "2006-02-01")
	_, _ = time.ParseDuration("2006-02-01")
	format := moment.Format
	_ = format("2006-02-01")
	parse := time.Parse
	_, _ = parse("2006-02-01", "2021-01-01")
	_ = time.Time.Format(moment, "2006-01-02")
}

// Parse is this package's, not time's.
func Parse(layout string) string {
	return layout
}
