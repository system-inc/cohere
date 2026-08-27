package next

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utilities/jsx"
)

var messageNoHtmlLinkForPages = rule.Message{
	Id: "noHtmlLinkForPages",
	Description: "This is a raw <a> element pointing at an internal route. Use `Link` from " +
		"`next/link`, which navigates on the client and prefetches the destination. A plain <a> " +
		"does a full document load instead, so the application is torn down and rebuilt and every " +
		"piece of in-memory state goes with it.",
}

// NoHtmlLinkForPages flags a raw <a> whose href is a string literal naming an internal route.
//
//	valid:   <Link href="/about">About</Link>
//	valid:   <a href="https://example.com">External</a>
//	valid:   <a href="/about" target="_blank">About</a>
//	valid:   <a href={dynamicLink}>Dynamic</a>
//	invalid: <a href="/about">About</a>
//	invalid: <a href="about">About</a>
//
// Ported from oxc's `no_html_link_for_pages.rs`.
//
// # Two upstreams disagree under this name, and this is the louder one
//
// `@next/next/no-html-link-for-pages` and oxlint implement different rules here. `@next` never
// decides without the filesystem: it reads `pages/`, `src/pages/`, `app/` and `src/app/` off disk,
// builds a route regex per file it finds, and reports only where an href matches a real route. With
// neither directory present it warns and returns an empty visitor, disabling itself entirely.
//
// oxlint decides from the href alone. It has no route model beyond "does this look internal", so it
// reports on relative hrefs and on hrefs that resolve to no page at all, both of which `@next`
// ignores. That difference is the common case rather than an edge: `about`, `./about` and
// `../contact` all report here and are all silent under `@next`.
//
// oxlint's spelling is what is ported, and the direction is deliberate. The differential harness
// compares verify against oxlint, so a rule faithful to `@next` would read as a disagreement on
// every relative href and the instrument could not separate that from a defect. `@next`'s failure
// mode is also the one this project exists to prevent: registered, enabled, and silently reporting
// nothing because a directory was not where it looked. Over-reporting is visible and someone tunes
// it; under-reporting is invisible and nobody tunes what they cannot see.
//
// Note that step 2b does not apply to the filesystem walk here. Upstream's directory read is not a
// workaround for a constraint we lack, it is the other rule's route model, and the ported rule does
// not have one.
//
// # This is not the same rule as structure/react-no-anchor-element
//
// That rule reports every raw <a> as a house-style matter and names our own Link component as the
// remedy. This one reports a subset of anchors, by href, and names `next/link`. Neither supersedes
// the other: measured against the release binary, oxlint reports an internal <a> inside
// `components/navigation/Link.tsx`, which `react-no-anchor-element` exempts by design because the
// Link component is the one place a raw anchor belongs. So each rule reports at least one input the
// other does not.
//
// # What is exempt, and why each exemption is upstream's rather than ours
//
// A `target="_blank"` link opens a new document by definition, so client-side navigation has
// nothing to offer it. A `download` attribute means the href is a file to save rather than a route
// to visit, and presence alone exempts, since the idiomatic spelling is bare. An href written as an
// expression is not read at all: upstream declines to guess at a value it would have to evaluate,
// which is the same judgment `jsx.StringAttributeValue` already encodes.
var NoHtmlLinkForPages = rule.Rule{
	// No family prefix. The config writes `nextjs/no-html-link-for-pages` and matching strips the
	// namespace on a `/` boundary, so a prefixed name matches nothing and runs on no files.
	Name: "no-html-link-for-pages",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		report := func(node *ast.Node) {
			tagName, attributes := jsx.ElementParts(node)
			if !jsx.IsIntrinsicElementNamed(tagName, "a") {
				return
			}

			// Both exemptions are asked before the href is read, matching upstream's order. The
			// order is not observable here, since neither exemption depends on the href, but
			// keeping it makes the two files diffable.
			if targetValue, found := jsx.StringAttributeValue(attributes, "target", jsx.MatchExactly); found &&
				targetValue == "_blank" {
				return
			}
			if jsx.HasAttributeNamed(attributes, "download", jsx.MatchExactly) {
				return
			}

			// An absent href and an href whose value is an expression collapse to the same answer.
			// Upstream separates them, returning early with no href attribute and then evaluating
			// a non-string-literal value to false, and the two paths reach one verdict: silence.
			//
			// The found flag is deliberately not tested, and a mutation sweep is what established
			// that it must not be. `StringAttributeValue` returns an empty string on every path
			// where the flag is false, measured across an absent attribute, an expression value, a
			// bare attribute, a spread, a braced string literal and a namespaced name, so a
			// `!found` term is subsumed by the empty-string check inside `isInternalPageLink` and
			// no input can distinguish the two spellings. Written with the term, the sweep reports
			// it as a survivor forever, which reads as a fixture blind spot rather than as the
			// dead condition it is.
			href, _ := jsx.StringAttributeValue(attributes, "href", jsx.MatchExactly)
			if !isInternalPageLink(href) {
				return
			}

			ctx.ReportNode(node, messageNoHtmlLinkForPages)
		}

		return rule.Listeners{
			ast.KindJsxOpeningElement:     report,
			ast.KindJsxSelfClosingElement: report,
		}
	},
}

// isInternalPageLink reports whether an href looks like a route within this application.
//
// This is upstream's whole route model, and it is a shape test rather than a lookup: nothing here
// asks whether the destination exists. Reproduced exactly, including the part that reads as
// redundant.
//
// The scheme test at the end is the load-bearing half. It asks whether the text before the first
// `/` contains a colon, which is how an unlisted protocol such as `data:` or `custom:` declines
// without being named. That also means a bare `foo:bar` with no slash at all declines, and that is
// upstream's answer rather than an accident of the port.
//
// # Most of this function is provably dead, and all of it is kept
//
// Only three of these guards decide anything. The protocol-relative test, the hash test and the
// empty test are load-bearing, and so is the scheme test at the end. Everything else is subsumed by
// the scheme test, because the thing it rejects already carries a colon in its first segment:
//
//	the `http://` and `https://` guard      first segment is `http:` or `https:`
//	the mailto, tel, ftp and file guard     first segment is `mailto:`, `tel:`, `ftp:`, `file:`
//	the leading-slash term at the end       an href starting `/` has an empty first segment,
//	                                        which contains no colon, so the second term already
//	                                        answers true
//
// The final expression also repeats the `//` test the second guard answered, so it cannot be
// reached with a protocol-relative href.
//
// These are measured rather than argued, because a mutation sweep reported all three as surviving
// mutants and the comfortable reading of a survivor is a fixture blind spot. Each guard was removed
// in turn and both spellings run over every string up to length six drawn from an alphabet holding
// every character any guard here examines, about seven and a half million inputs each. Not one
// input changed answer in any of the three. They are subsumed branches and no fixture can catch
// them, which is a different finding from a gap in the corpus and wants the opposite response.
//
// All of it is kept anyway. These are upstream's lines, deleting them changes no verdict, and a
// reader diffing the two files should find them the same rather than find a tightening here that
// has to be re-derived before it can be trusted. What deletion would buy is a cleaner sweep; what
// it would cost is the property that this function is upstream's text. The measurement is recorded
// here instead, so the next reader meets the survivors with the answer already attached rather than
// spending the afternoon rediscovering it.
func isInternalPageLink(href string) bool {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return false
	}
	if strings.HasPrefix(href, "//") {
		return false
	}
	if strings.HasPrefix(href, "mailto:") ||
		strings.HasPrefix(href, "tel:") ||
		strings.HasPrefix(href, "ftp:") ||
		strings.HasPrefix(href, "file:") {
		return false
	}
	if strings.HasPrefix(href, "#") {
		return false
	}
	if href == "" {
		return false
	}

	firstSegment := href
	if index := strings.Index(href, "/"); index >= 0 {
		firstSegment = href[:index]
	}
	return strings.HasPrefix(href, "/") ||
		(!strings.Contains(firstSegment, ":") && !strings.HasPrefix(href, "//"))
}
