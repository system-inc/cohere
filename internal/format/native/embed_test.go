package native

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/prettier"
)

// TestEmbeddedCodeMatchesTheFork formats markdown whose fences embed TypeScript, JavaScript and JSON,
// natively and through the embedded Prettier fork, and requires the same bytes. It covers what an
// embedded printer is told about its host: upstream's textToDoc sets parentParser on every embed
// (src/main/multiparser.js), and the JavaScript printer reads it to drop the semicolon after the only
// JSX element in a markdown code block (semicolon/semicolon.js).
func TestEmbeddedCodeMatchesTheFork(t *testing.T) {
	source := "# Probe\n\n" +
		"```tsx\n<Component   label=\"one\" onChange={(value) => setValue(value)} />\n```\n\n" +
		"```jsx\n<div className='x'>{ value }</div>\n```\n\n" +
		"```tsx\nconst a = <A />\nconst b = 1\n```\n\n" +
		"```ts\nfunction identity<T,>(value: T) { return value }\n```\n\n" +
		"```json\n{ \"a\": 1, b: [1,2,], }\n```\n\n" +
		"- item\n\n  ```tsx\n  <Nested prop={1} />\n  ```\n"
	options := prettier.DefaultOptions()
	oracle, err := prettier.New(options)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := oracle.Format("Probe.md", source)
	if err != nil {
		t.Fatal(err)
	}
	// The fork's own output, asserted, so a native printer and an oracle that both lost the rule cannot
	// agree their way past it: the lone JSX elements have no semicolon, the two statements keep theirs.
	for _, substring := range []string{
		"onChange={(value) => setValue(value)} />\n```",
		"<div className=\"x\">{value}</div>\n```",
		"const a = <A />;\nconst b = 1;",
		"    <Nested prop={1} />\n    ```",
	} {
		if !strings.Contains(expected, substring) {
			t.Fatalf("the fork's output lacks %q:\n%s", substring, expected)
		}
	}
	actual, err := Formatter{Options: options}.Format("Probe.md", source)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("--- fork\n%s--- native\n%s", expected, actual)
	}
}
