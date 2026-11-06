package native

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// TestEmbeddedCodeMatchesTheFork formats markdown whose fences embed TypeScript, JavaScript and JSON,
// natively and through the embedded Prettier fork, and requires the same bytes. It covers what an
// embedded printer is told about its host: upstream's textToDoc sets parentParser on every embed
// (src/main/multiparser.js), and the JavaScript printer reads it to drop the semicolon after the only
// JSX element in a markdown code block (semicolon/semicolon.js).
func TestEmbeddedCodeMatchesTheFork(test *testing.T) {
	test.Parallel()
	source := "# Probe\n\n" +
		"```tsx\n<Component   label=\"one\" onChange={(value) => setValue(value)} />\n```\n\n" +
		"```jsx\n<div className='x'>{ value }</div>\n```\n\n" +
		"```tsx\nconst a = <A />\nconst b = 1\n```\n\n" +
		"```ts\nfunction identity<T,>(value: T) { return value }\n```\n\n" +
		"```json\n{ \"a\": 1, b: [1,2,], }\n```\n\n" +
		"- item\n\n  ```tsx\n  <Nested prop={1} />\n  ```\n"
	options := format_options.Default()
	oracle, newPrettierError := prettier.New(options)
	if newPrettierError != nil {
		test.Fatal(newPrettierError)
	}
	expected, formatError := oracle.Format("Probe.md", source)
	if formatError != nil {
		test.Fatal(formatError)
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
			test.Fatalf("the fork's output lacks %q:\n%s", substring, expected)
		}
	}
	actual, formatError := Formatter{Options: options}.Format("Probe.md", source)
	if formatError != nil {
		test.Fatal(formatError)
	}
	if actual != expected {
		test.Fatalf("--- fork\n%s--- native\n%s", expected, actual)
	}
}

// graphqlTemplatesSource is unformatted GraphQL in TypeScript templates, the input the corpus cannot
// supply: its gql templates are all already formatted, so they read the same whether the embed works
// or prints them as written.
const graphqlTemplatesSource = `import gql from 'graphql-tag';

const fragment = gql` + "`fragment   UserFields on User{id name  email profile{avatar(size:LARGE) bio}}`" + `;

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

const mutation = graphql` +
	"`mutation($input:CreatePostInput!){createPost(input:$input){post{id}errors{field message}}}`" +
	`;

const marked = /* GraphQL */ ` + "`" + `
  query { viewer { login } }
` + "`" + `;

const onlyComments = gql` + "`" + `
  # nothing but a comment

  # and another
` + "`" + `;

const blank = gql` + "`   `" + `;
`

// cssTemplatesSource is unformatted CSS in styled-components and css templates, with expressions in
// a selector, a value, a property name and a declaration of their own: what embed/css.js replaces with
// @prettier-placeholder-N-id tokens before formatting the template as scss.
const cssTemplatesSource = `import styled, { css } from 'styled-components';

const Button = styled.button` + "`" + `
color:red;background:${(properties) => properties.background};
  ${Icon}:hover{opacity:.5}
  &:hover , &:focus{ color : blue }
  ${(properties) => properties.disabled && css` + "`opacity:0.4;cursor:not-allowed`" + `}
  margin-${direction}:4px;
  // an scss line comment
  @media (max-width:${breakpoint}px){padding:0 ${spacing}px}
` + "`" + `;

const Title = styled(Heading).attrs({ level: 2 })` +
	"`font-size:2EM;line-height:1.50;transition:opacity .2s ease-in-out,transform .2s ease-in-out," +
	"color .2s ease-in-out,background-color .2s ease-in-out`" +
	`;

const empty = css` + "`  `" + `;
`

// TestCssTemplatesMatchTheFork is the css embed's acceptance fixture: styled-components and css
// templates formatted through the native scss parser and CSS printer, against the fork, at two widths.
// With the embed not recognizing styled templates, they print as written, and this fails.
func TestCssTemplatesMatchTheFork(test *testing.T) {
	test.Parallel()
	for _, width := range []int{120, 80} {
		options := format_options.Default()
		options.PrintWidth = width
		oracle, newPrettierError := prettier.New(options)
		if newPrettierError != nil {
			test.Fatal(newPrettierError)
		}
		expected, formatError := oracle.Format("Probe.tsx", cssTemplatesSource)
		if formatError != nil {
			test.Fatal(formatError)
		}
		// The fork really formatted the CSS.
		for _, substring := range []string{"    color: red;\n", "font-size: 2em;", "const empty = css``;"} {
			if !strings.Contains(expected, substring) {
				test.Fatalf("printWidth %d: the fork's output lacks %q:\n%s", width, substring, expected)
			}
		}
		actual, formatError := Formatter{Options: options}.Format("Probe.tsx", cssTemplatesSource)
		if formatError != nil {
			test.Fatal(formatError)
		}
		if actual != expected {
			test.Fatalf("printWidth %d:\n--- fork\n%s--- native\n%s", width, expected, actual)
		}
	}
}

// TestGraphqlTemplatesMatchTheFork is the embed's acceptance fixture: gql and graphql templates and a
// /* GraphQL */ template, formatted through the native GraphQL printer, against the fork. With the
// embed not recognizing these templates, they print as written, and this fails.
func TestGraphqlTemplatesMatchTheFork(test *testing.T) {
	test.Parallel()
	for _, width := range []int{120, 80} {
		options := format_options.Default()
		options.PrintWidth = width
		oracle, newPrettierError := prettier.New(options)
		if newPrettierError != nil {
			test.Fatal(newPrettierError)
		}
		expected, formatError := oracle.Format("Probe.ts", graphqlTemplatesSource)
		if formatError != nil {
			test.Fatal(formatError)
		}
		// The fork really formatted the GraphQL: the cramped fragment is broken out.
		for _, substring := range []string{
			"fragment UserFields on User {\n",
			"mutation ($input: CreatePostInput!) {",
			"const blank = gql``;",
		} {
			if !strings.Contains(expected, substring) {
				test.Fatalf("printWidth %d: the fork's output lacks %q:\n%s", width, substring, expected)
			}
		}
		actual, formatError := Formatter{Options: options}.Format("Probe.ts", graphqlTemplatesSource)
		if formatError != nil {
			test.Fatal(formatError)
		}
		if actual != expected {
			test.Fatalf("printWidth %d:\n--- fork\n%s--- native\n%s", width, expected, actual)
		}
	}
}
