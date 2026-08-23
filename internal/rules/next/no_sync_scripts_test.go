package next

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

func TestNoSyncScriptsReports(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name:   "src with no async or defer",
			source: `export const C = () => <div><script src="https://blah.com"></script></div>;`,
		},
		{
			name:   "self closing form",
			source: `export const C = () => <script src="/a.js" />;`,
		},
		{
			// Another attribute being present must not read as async or defer being present.
			name:   "src with an unrelated attribute",
			source: `export const C = () => <script src="/a.js" type="text/javascript" />;`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, NoSyncScripts, "Component.tsx", testCase.source)
			ruletest.ExpectFindings(t, result, messageNoSyncScripts.Id)
		})
	}
}

func TestNoSyncScriptsIsSilent(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			// The idiomatic spelling of a boolean attribute has no initializer at all. A port that
			// read the value rather than the presence would find nothing here and report.
			name:   "bare async",
			source: `export const C = () => <script src="/a.js" async />;`,
		},
		{
			name:   "bare defer",
			source: `export const C = () => <script src="/a.js" defer />;`,
		},
		{
			name:   "async with an explicit value",
			source: `export const C = () => <script src="/a.js" async={true} />;`,
		},
		{
			// An inline script has no fetch to defer, so async and defer would do nothing on it.
			name:   "inline script with no src",
			source: `export const C = () => <script>{"var a = 1;"}</script>;`,
		},
		{
			name:   "spread attributes",
			source: `export const C = (props) => <script {...props} />;`,
		},
		{
			name:   "member expression name",
			source: `export const C = () => <Foo.script src="/a.js" />;`,
		},
		{
			name: "the next/script component",
			source: `import Script from 'next/script';
export const C = () => <Script src="/a.js" />;`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, NoSyncScripts, "Component.tsx", testCase.source)
			ruletest.ExpectClean(t, result)
		})
	}
}
