package next

import (
	"testing"

	"github.com/system-inc/verify/internal/rule_testing"
)

func TestGoogleFontDisplayReportsMissing(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name:   "no display parameter",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css2?family=Krona+One" />;`,
		},
		{
			// No query string at all reaches the same finding by a different path through the
			// query reader, so it is worth its own case.
			name:   "no query string",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css2" />;`,
		},
		{
			// A bare key with no `=` is skipped rather than read as an empty value, so this is
			// still a missing display rather than a present one.
			name:   "display written with no value",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css2?family=Krona+One&display" />;`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, GoogleFontDisplay, "Component.tsx", testCase.source)
			rule_testing.ExpectFindings(t, result, messageGoogleFontDisplayMissing.Id)
		})
	}
}

func TestGoogleFontDisplayReportsNotRecommended(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name:   "display block",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css2?family=Krona+One&display=block" />;`,
		},
		{
			name:   "display auto",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css2?family=Krona+One&display=auto" />;`,
		},
		{
			// oxc's own fail case, and the reason the query is parsed rather than pattern matched:
			// a port that assumed `family` came first would miss this entirely.
			name:   "display before family",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css2?display=fallback&family=Krona+One" />;`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, GoogleFontDisplay, "Component.tsx", testCase.source)
			rule_testing.ExpectFindings(t, result, messageGoogleFontDisplayNotRecommended.Id)
		})
	}
}

func TestGoogleFontDisplayIsSilent(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name:   "display optional",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css2?family=Krona+One&display=optional" />;`,
		},
		{
			name:   "display swap on the css1 endpoint",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css?family=Krona+One&display=swap" />;`,
		},
		{
			// An unrecognised value passes upstream. The rule reports the three it knows are
			// text-blocking and makes no claim about anything else.
			name:   "an unrecognised display value",
			source: `export const Test = () => <link href="https://fonts.googleapis.com/css2?family=Krona+One&display=something" />;`,
		},
		{
			name:   "a different host",
			source: `export const Test = () => <link href="https://example.com/css?family=Krona+One" />;`,
		},
		{
			// The font host rather than the stylesheet host. Only the latter serves a css query.
			name:   "the gstatic host",
			source: `export const Test = () => <link rel="preconnect" href="https://fonts.gstatic.com" />;`,
		},
		{
			name:   "computed href",
			source: `export const Test = () => <link href={fontUrl} />;`,
		},
		{
			name:   "spread attributes",
			source: `export const Test = (props) => <link {...props} />;`,
		},
		{
			name:   "member expression name",
			source: `export const Test = () => <Foo.link href="https://fonts.googleapis.com/css2?family=Krona+One" />;`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, GoogleFontDisplay, "Component.tsx", testCase.source)
			rule_testing.ExpectClean(t, result)
		})
	}
}
