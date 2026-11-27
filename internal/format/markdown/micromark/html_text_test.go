package micromark

// htmlTextFixtures exercise core html-text.js. Through the fork's extension set core htmlText is never
// what matches: the fork's copy is tried first on `<` and accepts the same inputs, so core only ever runs
// after it has already failed. They were chosen for a parse without the override
// (htmlTextOverrideExtension left out), where core is the one that wins.
//
// They avoid the constructs still pending, outside a matched tag (inside one, htmlText consumes the
// characters and nothing else is tried): attention (`*`, `_`), label ends and so any `[` or `]` (an
// unmatched `[` needs labelEnd's resolveAll to fall back to data), definitions, lists (no line starts with
// `-`, `+`, `*` or a digit and a dot), math (`$`), liquid (`{`), wiki links, strikethrough (`~`), tables
// (`|`), footnotes, the GFM autolink literals (no `www.`, `http://` or `a@b.c` left over by a failure) and
// htmlFlow: every first line starts with text, and no later line starts with a `<` that a kind 1 to 6
// HTML block could open on (kind 7 cannot interrupt a paragraph). CommonMark examples 618, 619 (a failure
// leaves `_` or `*`) and 627 (`$` around a failure would be math) are left out for that reason.
var htmlTextFixtures = []string{
	// CommonMark 0.31.2 "Raw HTML" examples, the ones that stay inside ported constructs.
	"<a><bab><c2c>\n",
	"<a/><b2/>\n",
	"<a  /><b2\ndata=\"foo\" >\n",
	"<a foo=\"bar\" bam = 'baz <em>\"</em>'\n_boolean zoop:33=zoop:33 />\n",
	"Foo <responsive-image src=\"foo.jpg\" />\n",
	"<a href=\"hi'> <a href=hi'>\n",
	"< a><\nfoo><bar/ >\n<foo bar=baz\nbim!bop />\n",
	"<a href='bar'title=title>\n",
	"</a></foo >\n",
	"</a href=\"foo\">\n",
	"foo <!-- this is a --\ncomment - with hyphens -->\n",
	"foo <!--> foo -->\n\nfoo <!---> foo -->\n",
	"foo <!ELEMENT br EMPTY>\n",
	"foo <![CDATA[>&<]]>\n",
	"foo <a href=\"&ouml;\">\n",
	"foo <a href=\"\\*\">\n",
	"<a href=\"\\\"\">\n",

	// open: what may follow `<`.
	"a <",
	"a <\nb",
	"a <1> b",
	"a < b> c",
	"a <é> b",
	"a <𝔸> b",
	"a <b:c> d",

	// declarationOpen, commentOpenInside, comment, commentClose, commentEnd.
	"a <!> b",
	"a <!",
	"a <!-x> b",
	"a <!-",
	"a <!--",
	"a <!--> b",
	"a <!---> b",
	"a <!----> b",
	"a <!--b--> c",
	"a <!--b-c--> d",
	"a <!--b--c--> d",
	"a <!--b---> c",
	"a <!--b",
	"a <!--b-",
	"a <!--b--",
	"a <!--b\nc--> d",
	"a <!--b\r\nc--> d",
	"a <!--b\rc--> d",
	"a <!--b\n\n--> c",
	"a <!--é𝔸--> b",

	// cdataOpenInside, cdata, cdataClose, cdataEnd.
	"a <![CDATA[b]]> c",
	"a <![CDATA[]]> b",
	"a <![CDATA[b]c]]> d",
	"a <![CDATA[b]]]> c",
	"a <![CDATA[b]]c]]> d",
	"a <![CDATA[b\nc]]> d",
	"a <![CDATA[<b>]]> c",

	// declaration.
	"a <!DOCTYPE html> b",
	"a <!b> c",
	"a <!b",
	"a <!b\nc> d",
	"a <!1> b",

	// instruction, instructionClose.
	"a <?> b",
	"a <??> b",
	"a <?b?> c",
	"a <?b?c?> d",
	"a <?",
	"a <?b?",
	"a <?b\nc?> d",

	// tagCloseStart, tagClose, tagCloseBetween.
	"a </b> c",
	"a </b-1> c",
	"a </b \t> c",
	"a </b\n> c",
	"a </b c> d",
	"a </b/> c",
	"a </1> b",
	"a </> b",
	"a </",
	"a </b",
	"a </b ",

	// tagOpen.
	"a <b> c",
	"a <b-c1> d",
	"a <b/> c",
	"a <b /> c",
	"a <b/ > c",
	"a <b.c> d",
	"a <bé> c",
	"a <b",
	"a <b\n> c",

	// tagOpenBetween, tagOpenAttributeName, tagOpenAttributeNameAfter.
	"a <b c> d",
	"a <b :c> d",
	"a <b c d> e",
	"a <b c-d.e:f9> g",
	"a <b 1> c",
	"a <b c",
	"a <b c ",
	"a <b\nc=d> e",
	"a <b c\n=d> e",

	// tagOpenAttributeValueBefore, the quoted and unquoted values, tagOpenAttributeValueQuotedAfter.
	"a <b c=d> e",
	"a <b c = d> e",
	"a <b c=\"d\"> e",
	"a <b c='d'> e",
	"a <b c=\"d'e\"> f",
	"a <b c=\"\"> d",
	"a <b c=d/> e",
	"a <b c=\"d\"/> e",
	"a <b c=\"d\"e> f",
	"a <b c=> d",
	"a <b c=<> d",
	"a <b c==> d",
	"a <b c=`> d",
	"a <b c=d\"> e",
	"a <b c=d'e> f",
	"a <b c=d<e> f",
	"a <b c=d=e> f",
	"a <b c=d`e> f",
	"a <b c=é> d",
	"a <b c=\"𝔸\"> d",
	"a <b c=",
	"a <b c=d",
	"a <b c=\"d",
	"a <b c=\nd> e",
	"a <b c=d\n> e",
	"a <b c=\"d\ne\"> f",
	"a <b c=\"d\r\ne\"> f",
	"a <b c='d\re'> f",
	"a <b c=\"d\"\ne> f",
	"a <b\n\n> c",

	// lineEndingAfter: the next line's indent, a linePrefix of at most 3 columns here (factorySpace max 4).
	"a <b\n c> d",
	"a <b\n   c> d",
	"a <b\n    c> d",
	"a <b\n     c> d",
	"a <b\n\tc> d",
	"a <b\n \tc> d",
	"a <!--b\n   c--> d",
	"a <!--b\n     c--> d",
	"a <b c=\"d\n  e\"> f",
	"a <b c=\n   \"d\"> e",

	// Around it: other text, escapes, headings, block quotes, setext, several in a row.
	"a <b><ab:c> d",
	"a <ab:c><b> d",
	"a \\<b> c",
	"a `<b>` c",
	"a&amp;<b>&amp;c",
	"é<b>𝔸",
	"# a <b> c",
	"# a <b c=\"d\"> #",
	"> a <b\n> c=\"d\"> e",
	"> a <b c=\"d\n> e\"> f",
	"a <b c=\"d\n  e\">\n===",
	"a <b><c></b></c>",
	"a  \n<b> c",
	"a\\\n<b> c",
}
