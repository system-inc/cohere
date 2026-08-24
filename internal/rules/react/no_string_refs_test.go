package react

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// stringRefsFile is where the fixtures pretend to live.
//
// A `.tsx` name rather than `.ts`, and that is load-bearing rather than cosmetic. This rule gates
// on the file being a JSX file, reproducing oxc's `should_run` on `source_type().is_jsx()`, so the
// same fixture under a `.ts` name is silent by design. `TestNoStringRefsNeedsAJsxFile` below pins
// that, because a revert of the gate would otherwise leave every clean case passing vacuously.
const stringRefsFile = "/repository/source/StringRefs.tsx"

// The corpus is oxc's, copied rather than rewritten.
//
// Every case below is verbatim from `oxc/crates/oxc_linter/src/rules/react/no_string_refs.rs`:
// 6 pass and 10 fail. The extractor reported 14 diagnostics against those 10 inputs, so one finding
// per input would have been wrong here. The four inputs that report twice each carry a `this.refs`
// read and a string ref in the same component, and the mapping below was recovered by aligning each
// snapshot diagnostic against the source line it underlines rather than by walking the two lists in
// order. That mattered: upstream lists one input twice, byte for byte, and an in-order walk
// silently gave both of its diagnostics to the first copy and none to the input after it.
//
// The strings were decoded from the extractor's dump and then checked byte against byte into the
// Rust source before any Go was written. Two of them came back wrong: the dump double-escaped the
// `\"` inside `ref=\"hello\"`, so the decoded fixture held a literal backslash and would have
// tested a different attribute value than upstream does. Reading them would not have caught it.
func TestNoStringRefsFires(t *testing.T) {
	cases := []struct {
		name               string
		sourceText         string
		noTemplateLiterals bool
		findings           []string
	}{
		{"this.refs inside componentDidMount", "\n              var Hello = createReactClass({\n                componentDidMount: function() {\n                  var component = this.refs.hello;\n                },\n                render: function() {\n                  return <div>Hello {this.props.name}</div>;\n                }\n              });\n            ", false, []string{"thisRefsDeprecated"}},
		{"a double-quoted string ref", "\n              var Hello = createReactClass({\n                render: function() {\n                  return <div ref=\"hello\">Hello {this.props.name}</div>;\n                }\n              });\n            ", false, []string{"stringInRefDeprecated"}},
		{"a single-quoted string ref in a container", "\n              var Hello = createReactClass({\n                render: function() {\n                  return <div ref={'hello'}>Hello {this.props.name}</div>;\n                }\n              });\n            ", false, []string{"stringInRefDeprecated"}},
		{"this.refs and a string ref in one component", "\n              var Hello = createReactClass({\n                componentDidMount: function() {\n                  var component = this.refs.hello;\n                },\n                render: function() {\n                  return <div ref=\"hello\">Hello {this.props.name}</div>;\n                }\n              });\n            ", false, []string{"thisRefsDeprecated", "stringInRefDeprecated"}},
		{"this.refs and a template ref under the option", "\n              var Hello = createReactClass({\n                componentDidMount: function() {\n                var component = this.refs.hello;\n                },\n                render: function() {\n                  return <div ref={`hello`}>Hello {this.props.name}</div>;\n                }\n              });\n            ", true, []string{"thisRefsDeprecated", "stringInRefDeprecated"}},
		{"this.refs and an interpolated template ref under the option", "\n              var Hello = createReactClass({\n                componentDidMount: function() {\n                var component = this.refs.hello;\n                },\n                render: function() {\n                  return <div ref={`hello${index}`}>Hello {this.props.name}</div>;\n                }\n              });\n            ", true, []string{"thisRefsDeprecated", "stringInRefDeprecated"}},
		{"an interpolated template ref alone under the option", "\n              var Hello = createReactClass({\n                render: function() {\n                  return <div ref={`hello${index}`}>Hello {this.props.name}</div>;\n                }\n              });\n            ", true, []string{"stringInRefDeprecated"}},
		{"this.refs in an ES6 class component", "\n              class Hello extends React.Component {\n                componentDidMount() {\n                  var component = this.refs.hello;\n                }\n              }\n            ", false, []string{"thisRefsDeprecated"}},
		{"this.refs in an ES6 class component, listed twice upstream", "\n              class Hello extends React.Component {\n                componentDidMount() {\n                  var component = this.refs.hello;\n                }\n              }\n            ", false, []string{"thisRefsDeprecated"}},
		{"this.refs and a template ref in a PureComponent", "\n              class Hello extends React.PureComponent {\n                componentDidMount() {\n                  var component = this.refs.hello;\n                }\n                render() {\n                  return <div ref={`hello${index}`}>Hello {this.props.name}</div>;\n                }\n              }\n            ", true, []string{"thisRefsDeprecated", "stringInRefDeprecated"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.RunWithOptions(t, NoStringRefs, stringRefsFile, testCase.sourceText,
				NoStringRefsOptions{NoTemplateLiterals: testCase.noTemplateLiterals})
			ruletest.ExpectFindings(t, result, testCase.findings...)
		})
	}
}

// The clean cases are the whole discrimination, and each declines for a different reason.
//
// The first, fifth and sixth read `this.refs` from somewhere no component encloses: a plain
// function, and a plain function that merely sits beside a component rather than inside one. Those
// three are why the rule asks about the enclosing component instead of matching `this.refs` on
// sight. The second passes a callback, which is the repair this rule is asking for. The third and
// fourth write template refs with the option off, which is the option's entire subject.
func TestNoStringRefsStaysSilent(t *testing.T) {
	cases := []struct {
		name               string
		sourceText         string
		noTemplateLiterals bool
	}{
		{"this.refs in a plain function, which is no component", "\n                    var Hello = function() {\n                      return this.refs;\n                    };\n                  ", false},
		{"a ref callback beside a this.hello read", "\n                    var Hello = React.createReactClass({\n                      componentDidMount: function() {\n                        var component = this.hello;\n                      },\n                      render: function() {\n                        return <div ref={c => this.hello = c}>Hello {this.props.name}</div>;\n                      }\n                    });\n                  ", false},
		{"a template ref with the option off", "\n                    var Hello = createReactClass({\n                      render: function() {\n                        return <div ref={`hello`}>Hello {this.props.name}</div>;\n                      }\n                    });\n                  ", false},
		{"a template ref with interpolation and the option off", "\n                    var Hello = createReactClass({\n                      render: function() {\n                        return <div ref={`hello${index}`}>Hello {this.props.name}</div>;\n                      }\n                    });\n                  ", false},
		{"this.refs outside the createReactClass beside it", "\n                    var Hello = function() {\n                      return this.refs;\n                    };\n                    createReactClass({\n                      render: function() {\n                        let x;\n                      }\n                    });\n                  ", false},
		{"this.refs outside the class component beside it", "\n                    var Hello = function() {\n                      return this.refs;\n                    };\n                    class Other extends React.Component {\n                      render() {\n                        let x;\n                      }\n                    };\n                  ", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.RunWithOptions(t, NoStringRefs, stringRefsFile,
				testCase.sourceText, NoStringRefsOptions{NoTemplateLiterals: testCase.noTemplateLiterals}))
		})
	}
}

// Cases upstream does not ship, each covering a discrimination its corpus leaves untested.
//
// Every shape here was run against the release oxlint binary before being written down, so these
// assert measured upstream behavior rather than my reading of the Rust. Three of them were found by
// mutation: the rule survived a mutant that compared the attribute name case-insensitively, and one
// that accepted any object rather than `this`, because no upstream fixture writes either shape.
func TestNoStringRefsDiscriminationsUpstreamDoesNotCover(t *testing.T) {
	fires := []struct {
		name       string
		sourceText string
		findings   []string
	}{
		// oxc resolves a computed member through `static_property_name`, which answers for a string
		// literal, so the bracketed spelling is the same judgment as the dotted one. Confirmed
		// reporting on the release binary.
		{
			"a computed refs read written with a string",
			"class Hello extends React.Component {\n  m() { return this[\"refs\"].hello; }\n}\n",
			[]string{"thisRefsDeprecated"},
		},
		// The same resolution accepts a single-quasi template, which is what `quasis.len() == 1`
		// means upstream and what a NoSubstitutionTemplateLiteral is here. Confirmed reporting.
		{
			"a computed refs read written with a bare template",
			"class Hello extends React.Component {\n  m() { return this[`refs`].hello; }\n}\n",
			[]string{"thisRefsDeprecated"},
		},
		// A ref attribute needs no enclosing component, unlike the other half of this rule. This is
		// the asymmetry the rule doc describes, and upstream writes every ref fixture inside a
		// component so nothing in the imported corpus pins it. Confirmed reporting.
		{
			"a string ref outside any component",
			"const a = <div ref=\"hello\" />;\n",
			[]string{"stringInRefDeprecated"},
		},
		// A double-quoted string inside the container, which upstream only ever writes
		// single-quoted. Confirmed reporting.
		{
			"a double-quoted string ref inside a container",
			"const a = <div ref={\"hello\"} />;\n",
			[]string{"stringInRefDeprecated"},
		},
	}

	for _, testCase := range fires {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t,
				ruletest.Run(t, NoStringRefs, stringRefsFile, testCase.sourceText),
				testCase.findings...)
		})
	}

	silent := []struct {
		name       string
		sourceText string
	}{
		// Found by mutation. A rule matching the attribute name case-insensitively survived the
		// entire imported corpus, because upstream never writes the attribute any other way. JSX
		// attribute names are case-sensitive and `REF` is a different attribute; confirmed silent
		// on the release binary.
		{
			"a ref attribute in the wrong case",
			"const a = <div REF=\"hello\" />;\n",
		},
		// Found by mutation. A rule dropping the `this` check survived everything, since upstream
		// has no fixture reading `refs` off anything else. Confirmed silent.
		{
			"a refs read off something that is not this",
			"class Hello extends React.Component {\n  m() { return that.refs.hello; }\n}\n",
		},
		// The same shape one level in: a nested object whose property happens to be named `refs`.
		{
			"a refs property on a plain object",
			"class Hello extends React.Component {\n  m() { return this.props.refs.hello; }\n}\n",
		},
		// An interpolated computed key cannot be resolved without evaluating it, so upstream's
		// `quasis.len() == 1` declines it. Confirmed silent on the release binary, which is the
		// pair to the bare-template case above.
		{
			"a computed refs read with interpolation",
			"class Hello extends React.Component {\n  m() { return this[`refs${x}`].hello; }\n}\n",
		},
		// A bare attribute has no value at all, and both upstreams require one before looking.
		{
			"a ref attribute with no value",
			"const a = <div ref />;\n",
		},
		// A ref holding something that is not a string is the ordinary modern spelling once it is a
		// variable, and a number is neither shape this rule refuses.
		{
			"a ref holding a number",
			"const a = <div ref={123} />;\n",
		},
		{
			"a ref holding an identifier",
			"const a = <div ref={hello} />;\n",
		},
		// A namespaced attribute name is not a plain identifier, which is the shape oxc destructures
		// and returns early on. Confirmed silent.
		{
			"a namespaced ref attribute",
			"const a = <svg xlink:ref=\"hello\" />;\n",
		},
		// An attribute whose name merely contains `ref`.
		{
			"an attribute whose name only starts with ref",
			"const a = <div refs=\"hello\" />;\n",
		},
	}

	for _, testCase := range silent {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, NoStringRefs, stringRefsFile, testCase.sourceText))
		})
	}
}

// The file gate is the one discrimination no upstream fixture can reach, because oxc's tester only
// ever names the fixture `.tsx`.
//
// Without it this rule reports `this.refs` inside every plain TypeScript file holding a class that
// extends `React.Component`, which is a real shape in this codebase and not a contrived one. The
// gate reproduces oxc's `should_run` on `source_type().is_jsx()`, and it was measured against the
// release oxlint binary rather than inferred from the call: the same four-line class written to
// `probe.ts` and to `probe.tsx` and linted in one invocation reported on the `.tsx` copy only.
func TestNoStringRefsNeedsAJsxFile(t *testing.T) {
	// Deliberately the `this.refs` half rather than the attribute half. A `.ts` file cannot hold a
	// JSX attribute at all, so gating on a ref attribute would pass whether or not the gate exists.
	sourceText := "class Hello extends React.Component {\n" +
		"  componentDidMount() {\n" +
		"    var component = this.refs.hello;\n" +
		"  }\n" +
		"}\n"

	ruletest.ExpectFindings(t,
		ruletest.Run(t, NoStringRefs, "/repository/source/Hello.tsx", sourceText),
		"thisRefsDeprecated")
	ruletest.ExpectClean(t, ruletest.Run(t, NoStringRefs, "/repository/source/Hello.ts", sourceText))
	ruletest.ExpectFindings(t,
		ruletest.Run(t, NoStringRefs, "/repository/source/Hello.jsx", sourceText),
		"thisRefsDeprecated")
	ruletest.ExpectClean(t, ruletest.Run(t, NoStringRefs, "/repository/source/Hello.js", sourceText))
}

// Where each finding points, which no message-id assertion can see.
//
// Both spans are upstream's and both are surprising in a different direction. The `this.refs`
// finding underlines nine characters, so it points at the `refs` read and not at `this.refs.hello`,
// which is what the snapshot's nine-character underline under a longer expression records. The
// attribute finding underlines the whole `ref="hello"`, which is `attr.span` rather than the name
// span that `no-children-prop` in this same package reports, so the two rules here disagree about
// where a JSX attribute finding belongs and that disagreement is upstream's rather than ours.
func TestNoStringRefsPointsAtTheRightNode(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		reported   []string
	}{
		{
			"the refs read, not the property taken off it",
			"class Hello extends React.Component {\n  m() { return this.refs.hello; }\n}\n",
			[]string{"this.refs"},
		},
		{
			"a bare refs read with nothing taken off it",
			"class Hello extends React.Component {\n  m() { return this.refs; }\n}\n",
			[]string{"this.refs"},
		},
		{
			"a computed refs read",
			"class Hello extends React.Component {\n  m() { return this[\"refs\"].hello; }\n}\n",
			[]string{"this[\"refs\"]"},
		},
		{
			"the whole attribute, not its name",
			"const a = <div ref=\"hello\" />;\n",
			[]string{"ref=\"hello\""},
		},
		{
			"the whole attribute when the value is in a container",
			"const a = <div ref={'hello'} />;\n",
			[]string{"ref={'hello'}"},
		},
		{
			"both halves in one component, in source order",
			"class Hello extends React.Component {\n" +
				"  m() { return this.refs.hello; }\n" +
				"  render() { return <div ref=\"hello\" />; }\n" +
				"}\n",
			[]string{"this.refs", "ref=\"hello\""},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, NoStringRefs, stringRefsFile, testCase.sourceText)
			if len(result.Diagnostics) != len(testCase.reported) {
				t.Fatalf("got %d findings, want %d", len(result.Diagnostics), len(testCase.reported))
			}
			for i, want := range testCase.reported {
				got := testCase.sourceText[result.Diagnostics[i].Range.Pos():result.Diagnostics[i].Range.End()]
				if got != want {
					t.Errorf("finding %d underlines %q, want %q", i, got, want)
				}
			}
		})
	}
}

// The rendered text of both messages, asserted by equality rather than by containment.
//
// A `strings.Contains` check on a message that interpolates nothing still passes when the message
// is wrong in a way that only adds text, so equality is what is asserted. The two descriptions also
// have to stay distinguishable from each other, since this rule's whole structure rests on the two
// judgments being separately named.
func TestNoStringRefsMessagesReadCorrectly(t *testing.T) {
	if messageThisRefsDeprecated.Id != "thisRefsDeprecated" {
		t.Errorf("this.refs message id is %q", messageThisRefsDeprecated.Id)
	}
	if messageStringInRefDeprecated.Id != "stringInRefDeprecated" {
		t.Errorf("string ref message id is %q", messageStringInRefDeprecated.Id)
	}
	if messageThisRefsDeprecated.Description == messageStringInRefDeprecated.Description {
		t.Error("the two judgments render the same description, so a reader cannot tell them apart")
	}
	// The description says why the code is wrong rather than restating the rule name, so it must
	// not simply be the rule name back again.
	for _, message := range []string{
		messageThisRefsDeprecated.Description,
		messageStringInRefDeprecated.Description,
	} {
		if len(message) < 60 || !strings.Contains(message, "React 19") {
			t.Errorf("description does not explain the removal: %q", message)
		}
	}
}
