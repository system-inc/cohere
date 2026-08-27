package next

import (
	"testing"

	"github.com/system-inc/verify/internal/rule_testing"
)

// The upstream corpus, copied byte for byte from oxc's tester block and verified mechanically
// against the extractor's dump rather than by reading. One tester block, 21 pass and 6 fail, and
// the snapshot carries 6 diagnostics against 6 fail inputs, so every failing case reports exactly
// once.
var upstreamPassCases = []string{
	"<a href='https://example.com'>External Link</a>",
	"<a href='http://example.com'>External Link</a>",
	"<a href='mailto:contact@example.com'>Email</a>",
	"<a href='tel:+1234567890'>Phone</a>",
	"<a href='ftp://example.com'>FTP</a>",
	"<a href='file://path/to/file'>File</a>",
	"<a href='#section'>Jump to section</a>",
	"<a href='#'>Empty hash</a>",
	"<a href='//example.com'>Protocol-relative</a>",
	"<a>No href</a>",
	"<div href='/about'>Not an anchor</div>",
	"<Link href='/about'>About</Link>",
	"<a href=\"/about\" target=\"_blank\">About</a>",
	"<a target=\"_blank\" href=\"/about\">About</a>",
	"<a href=\"/about\" target=\"_blank\" rel=\"noopener noreferrer\">About</a>",
	"<a href=\"/static-file.csv\" download>Download CSV</a>",
	"<a href=\"/report.pdf\" download=\"report.pdf\">Download Report</a>",
	"<a download href=\"/data.json\">Download JSON</a>",
	"<a href={dynamicLink}>Dynamic</a>",
	"<a href={`/user/${userId}`}>User Profile</a>",
	"<a href={getUrl()}>Dynamic URL</a>",
}

var upstreamFailCases = []string{
	"<a href='/about'>About</a>",
	"<a href='/contact/us'>Contact Us</a>",
	"<a href='about'>About</a>",
	"<a href='../contact'>Contact</a>",
	"<a href='./about'>About</a>",
	"<a href='/'>Home</a>",
}

// A JSX fragment cannot stand alone as a source file, so every case is wrapped the same way. The
// wrapper is a plain arrow returning the element, which adds no anchors of its own.
func htmlLinkSource(element string) string {
	return "export const C = () => (" + element + ");"
}

func TestNoHtmlLinkForPagesReportsTheUpstreamCorpus(t *testing.T) {
	for _, element := range upstreamFailCases {
		t.Run(element, func(t *testing.T) {
			result := rule_testing.Run(t, NoHtmlLinkForPages, "pages/Index.tsx", htmlLinkSource(element))
			rule_testing.ExpectFindings(t, result, messageNoHtmlLinkForPages.Id)
		})
	}
}

func TestNoHtmlLinkForPagesIsSilentOnTheUpstreamCorpus(t *testing.T) {
	for _, element := range upstreamPassCases {
		t.Run(element, func(t *testing.T) {
			result := rule_testing.Run(t, NoHtmlLinkForPages, "pages/Index.tsx", htmlLinkSource(element))
			rule_testing.ExpectClean(t, result)
		})
	}
}

// Cases upstream does not write, each from reading our code rather than from a neighbouring rule.
func TestNoHtmlLinkForPagesReportsCasesUpstreamDoesNotWrite(t *testing.T) {
	cases := []struct {
		name    string
		element string
	}{
		{
			// A self-closing anchor never produces a JsxOpeningElement, so a rule listening only
			// to the paired form is silent on it. oxc anchors on JSXOpeningElement, which its
			// parser produces for both forms; our parser does not, so both listeners are required
			// and this case is what holds them there.
			name:    "self closing anchor",
			element: `<a href="/about" />`,
		},
		{
			// `target` set to anything other than _blank does not exempt. Upstream compares the
			// literal exactly, so only the one value is special.
			name:    "target set to something other than blank",
			element: `<a href="/about" target="_self">About</a>`,
		},
		{
			// A `target` written as an expression cannot be read as a string literal, so the
			// exemption does not apply and the anchor reports. This pins the direction: an
			// unreadable target is not treated as possibly-_blank.
			name:    "target written as an expression",
			element: `<a href="/about" target={windowTarget}>About</a>`,
		},
		{
			// The scheme test reads the text before the first slash. A path segment containing a
			// colon after the first slash therefore does not exempt, because only the first
			// segment is examined.
			name:    "colon in a later path segment",
			element: `<a href="/about:us">About</a>`,
		},
		{
			// A query-only href has no slash and no colon, so the first segment is the whole
			// string and it reports. Upstream writes no such case and this is where its shape test
			// lands.
			name:    "query only href",
			element: `<a href="?page=2">Next</a>`,
		},
		{
			// Attribute names are matched case-sensitively, so an uppercase TARGET does not
			// exempt and the anchor reports. Pinned against the release binary, which reports
			// here. A case-insensitive matcher would silence this, and a mutation sweep found
			// nothing else in the corpus could tell the two matchers apart.
			name:    "uppercase target does not exempt",
			element: `<a href="/about" TARGET="_blank">About</a>`,
		},
		{
			// Same for the download exemption, and also pinned against the release binary.
			name:    "uppercase download does not exempt",
			element: `<a href="/about" DOWNLOAD>About</a>`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, NoHtmlLinkForPages, "pages/Index.tsx", htmlLinkSource(testCase.element))
			rule_testing.ExpectFindings(t, result, messageNoHtmlLinkForPages.Id)
		})
	}
}

func TestNoHtmlLinkForPagesIsSilentOnCasesUpstreamDoesNotWrite(t *testing.T) {
	cases := []struct {
		name    string
		element string
	}{
		{
			// A member-expression tag is a component reference and never emits an HTML anchor, so
			// comparing the text alone would report code that renders no <a>. Pinned against the
			// release binary, which is silent here.
			name:    "member expression tag named a",
			element: `<Foo.a href="/about">About</Foo.a>`,
		},
		{
			// Upstream compares the tag name case-sensitively, so a capitalised component named A
			// is not the HTML element. Also pinned against the release binary.
			name:    "capitalised component",
			element: `<A href="/about">About</A>`,
		},
		{
			// An unlisted scheme declines through the colon test rather than by being named, which
			// is the only thing that test does that the explicit prefix checks do not.
			name:    "unlisted scheme",
			element: `<a href="data:text/plain,hi">Data</a>`,
		},
		{
			// No slash at all, and a colon in what is therefore the whole first segment. Upstream
			// declines this and it is the shape most likely to be read as a relative path.
			name:    "scheme like href with no slash",
			element: `<a href="custom:thing">Thing</a>`,
		},
		{
			// An empty href declines explicitly rather than falling through the shape test, which
			// would otherwise call it internal.
			name:    "empty href",
			element: `<a href="">Empty</a>`,
		},
		{
			// An element that spreads its attributes offers no href to read. The shelf helper
			// declines a spread rather than treating it as absent, and either way the answer is
			// silence, so this pins that a spread cannot be guessed at.
			name:    "spread attributes",
			element: `<a {...linkProperties}>Spread</a>`,
		},
		{
			// The download exemption is presence rather than value, so a bare attribute with no
			// initializer exempts. Upstream writes this too, but with the attribute after the
			// href; this writes it on a relative href, where the exemption is the only thing
			// standing between the input and a finding.
			name:    "bare download on a relative href",
			element: `<a href="about" download>About</a>`,
		},
		{
			// The href name is matched case-sensitively too, so an uppercase HREF is not read as
			// an href at all and the anchor has no route to judge. Pinned against the release
			// binary, which is silent here. This is the opposite direction from the two uppercase
			// cases above: there the exemption failed to apply and the anchor reported, here the
			// href itself goes unseen and the anchor does not.
			name:    "uppercase href is not an href",
			element: `<a HREF="/about">About</a>`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, NoHtmlLinkForPages, "pages/Index.tsx", htmlLinkSource(testCase.element))
			rule_testing.ExpectClean(t, result)
		})
	}
}

// The finding points at the opening element and not at the whole element, which is what upstream's
// snapshot underlines: `<a href='/about'>` is seventeen characters and the snapshot's label is
// seventeen wide. A rule reporting the JsxElement instead would pass every assertion above.
func TestNoHtmlLinkForPagesPointsAtTheOpeningElement(t *testing.T) {
	cases := []struct {
		name     string
		element  string
		reported string
	}{
		{
			name:     "paired element reports the opening tag alone",
			element:  `<a href='/about'>About</a>`,
			reported: `<a href='/about'>`,
		},
		{
			name:     "self closing element reports the whole element",
			element:  `<a href='/about' />`,
			reported: `<a href='/about' />`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			source := htmlLinkSource(testCase.element)
			result := rule_testing.Run(t, NoHtmlLinkForPages, "pages/Index.tsx", source)
			rule_testing.ExpectFindings(t, result, messageNoHtmlLinkForPages.Id)

			diagnostic := result.Diagnostics[0]
			reported := source[diagnostic.Range.Pos():diagnostic.Range.End()]
			if reported != testCase.reported {
				t.Errorf("finding points at %q, want %q", reported, testCase.reported)
			}
		})
	}
}

// The message the finding carries is asserted whole rather than by substring, because a substring
// predicate is weaker than the property it guards.
//
// This rule interpolates nothing, so there is no rendered text distinct from the declared message
// and no format verb that could be mismatched. What the assertion holds instead is that the finding
// carries the message this rule declares and not a neighbour's: the package reports several
// different raw elements through the same shape, and a report site handed the wrong message value
// satisfies an identifier assertion whenever the two identifiers happen to match.
func TestNoHtmlLinkForPagesCarriesItsOwnMessage(t *testing.T) {
	result := rule_testing.Run(t, NoHtmlLinkForPages, "pages/Index.tsx", htmlLinkSource(`<a href='/about'>About</a>`))
	rule_testing.ExpectFindings(t, result, messageNoHtmlLinkForPages.Id)

	wantDescription := "This is a raw <a> element pointing at an internal route. Use `Link` from " +
		"`next/link`, which navigates on the client and prefetches the destination. A plain <a> " +
		"does a full document load instead, so the application is torn down and rebuilt and every " +
		"piece of in-memory state goes with it."
	if got := result.Diagnostics[0].Message.Description; got != wantDescription {
		t.Errorf("description is %q, want %q", got, wantDescription)
	}
	if got := result.Diagnostics[0].Message.Id; got != "noHtmlLinkForPages" {
		t.Errorf("message id is %q, want %q", got, "noHtmlLinkForPages")
	}
}
