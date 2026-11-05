package next

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

func TestGoogleFontDisplayReportsMissing(test *testing.T) {
	test.Parallel()

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
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, GoogleFontDisplay, "Component.tsx", testCase.source)
			rule_testing.ExpectFindings(test, result, messageGoogleFontDisplayMissing.Id)
		})
	}
}

func TestGoogleFontDisplayReportsNotRecommended(test *testing.T) {
	test.Parallel()

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
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, GoogleFontDisplay, "Component.tsx", testCase.source)
			rule_testing.ExpectFindings(test, result, messageGoogleFontDisplayNotRecommended.Id)
		})
	}
}

func TestGoogleFontDisplayIsSilent(test *testing.T) {
	test.Parallel()

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
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, GoogleFontDisplay, "Component.tsx", testCase.source)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestGoogleFontDisplayDecodesEntities reads its attribute as ESLint does, with HTML entities decoded: typescript-estree decodes a
// JSX attribute string before any rule sees it. Each row's verdict is the installed
// @next/eslint-plugin-next 16.3.1's under the typescript-eslint parser (#51y9jh2).
func TestGoogleFontDisplayDecodesEntities(test *testing.T) {
	test.Parallel()

	for _, row := range []struct {
		name   string
		source string
		ids    []string
	}{
		{`an encoded ampersand before display=block`, `export const Test = () => <link href="https://fonts.googleapis.com/css2?family=Krona+One&amp;display=block" />;`, []string{messageGoogleFontDisplayNotRecommended.Id}},
		{`an encoded ampersand before display=optional`, `export const Test = () => <link href="https://fonts.googleapis.com/css2?family=Krona+One&amp;display=optional" />;`, []string{}},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, GoogleFontDisplay, "Component.tsx", row.source)
			if len(row.ids) == 0 {
				rule_testing.ExpectClean(test, result)
				return
			}
			rule_testing.ExpectFindings(test, result, row.ids...)
		})
	}
}
