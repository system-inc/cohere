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

// graphqlTemplatesSource is unformatted GraphQL in TypeScript templates, the input the corpus cannot
// supply: its gql templates are all already formatted, so they read the same whether the embed works
// or prints them as written.
const graphqlTemplatesSource = `import gql from 'graphql-tag';

const fragment = gql` + "`" + `fragment   UserFields on User{id name  email profile{avatar(size:LARGE) bio}}` + "`" + `;

export const Query = gql` + "`" + `
  # leading comment
  query UserQuery($id: ID!, $first: Int = 10, $after: String, $includeArchivedItemsInTheResultSet: Boolean = false) @cached(ttl: 60) {
    user(id: $id) { ...UserFields
      posts(first: $first, after: $after, includeArchived: $includeArchivedItemsInTheResultSet, orderBy: {field: CREATED_AT, direction: DESC}) {
        edges { node { id title  # trailing comment
          tags } }
        pageInfo{hasNextPage endCursor}
      }
    }
  }

  ${fragment}
` + "`" + `;

const mutation = graphql` + "`" + `mutation($input:CreatePostInput!){createPost(input:$input){post{id}errors{field message}}}` + "`" + `;

const marked = /* GraphQL */ ` + "`" + `
  query { viewer { login } }
` + "`" + `;

const onlyComments = gql` + "`" + `
  # nothing but a comment

  # and another
` + "`" + `;

const blank = gql` + "`" + `   ` + "`" + `;
`

// TestGraphqlTemplatesMatchTheFork is the embed's acceptance fixture: gql and graphql templates and a
// /* GraphQL */ template, formatted through the native GraphQL printer, against the fork. With the
// embed not recognizing these templates, they print as written, and this fails.
func TestGraphqlTemplatesMatchTheFork(t *testing.T) {
	for _, width := range []int{120, 80} {
		options := prettier.DefaultOptions()
		options.PrintWidth = width
		oracle, err := prettier.New(options)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := oracle.Format("Probe.ts", graphqlTemplatesSource)
		if err != nil {
			t.Fatal(err)
		}
		// The fork really formatted the GraphQL: the cramped fragment is broken out.
		for _, substring := range []string{"fragment UserFields on User {\n", "mutation ($input: CreatePostInput!) {", "const blank = gql``;"} {
			if !strings.Contains(expected, substring) {
				t.Fatalf("printWidth %d: the fork's output lacks %q:\n%s", width, substring, expected)
			}
		}
		actual, err := Formatter{Options: options}.Format("Probe.ts", graphqlTemplatesSource)
		if err != nil {
			t.Fatal(err)
		}
		if actual != expected {
			t.Fatalf("printWidth %d:\n--- fork\n%s--- native\n%s", width, expected, actual)
		}
	}
}
