package typescript

// preferNullishCoalescingIfCases are upstream's invalid cases that report from the if-statement path,
// two of which also report a `||`, in upstream's order.
var preferNullishCoalescingIfCases = []preferNullishCoalescingReplayedCase{
	{name: "upstream invalid[326]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (!foo) {
    foo = makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (!foo) {
    foo = makeFoo();
  }`, suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[327]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) {
    foo = makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo == null) {
    foo = makeFoo();
  }`, suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[328]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) {
    foo ??= makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo == null) {
    foo ??= makeFoo();
  }`, suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[329]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) {
    foo ||= makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo == null) {
    foo ||= makeFoo();
  }`, suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
}
`},
		{
			id:       "preferNullishOverOr",
			line:     6,
			column:   9,
			reported: `||=`,
			suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) {
    foo ??= makeFoo();
  }
}
`,
		},
	}},
	{name: "upstream invalid[330]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo === null) {
    foo = makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo === null) {
    foo = makeFoo();
  }`, suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[331]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) foo = makeFoo();
  const bar = 42;
  return bar;
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverAssignment",
			line:     5,
			column:   3,
			reported: `if (foo == null) foo = makeFoo();`,
			suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
  const bar = 42;
  return bar;
}
`,
		},
	}},
	{name: "upstream invalid[332]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) foo ??= makeFoo();
  const bar = 42;
  return bar;
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverAssignment",
			line:     5,
			column:   3,
			reported: `if (foo == null) foo ??= makeFoo();`,
			suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
  const bar = 42;
  return bar;
}
`,
		},
	}},
	{name: "upstream invalid[333]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) foo ||= makeFoo();
  const bar = 42;
  return bar;
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverAssignment",
			line:     5,
			column:   3,
			reported: `if (foo == null) foo ||= makeFoo();`,
			suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
  const bar = 42;
  return bar;
}
`,
		},
		{
			id:       "preferNullishOverOr",
			line:     5,
			column:   24,
			reported: `||=`,
			suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo == null) foo ??= makeFoo();
  const bar = 42;
  return bar;
}
`,
		},
	}},
	{name: "upstream invalid[334]", source: `
declare let foo: { a: string } | undefined;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo === undefined) {
    foo = makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo === undefined) {
    foo = makeFoo();
  }`, suggested: `declare let foo: { a: string } | undefined;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[335]", source: `
declare let foo: { a: string } | null | undefined;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  if (foo === undefined || foo === null) {
    foo = makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo === undefined || foo === null) {
    foo = makeFoo();
  }`, suggested: `declare let foo: { a: string } | null | undefined;
declare function makeFoo(): { a: string };

function lazyInitialize() {
  foo ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[336]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): string;

function lazyInitialize() {
  if (foo.a == null) {
    foo.a = makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo.a == null) {
    foo.a = makeFoo();
  }`, suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): string;

function lazyInitialize() {
  foo.a ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[337]", source: `
declare let foo: { a: string } | null;
declare function makeFoo(): string;

function lazyInitialize() {
  if (foo?.a == null) {
    foo.a = makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo?.a == null) {
    foo.a = makeFoo();
  }`, suggested: `declare let foo: { a: string } | null;
declare function makeFoo(): string;

function lazyInitialize() {
  foo.a ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[338]", source: `
declare let foo: string | null;
declare function makeFoo(): string;

function lazyInitialize() {
  if (foo == null) {
    // comment
    foo = makeFoo();
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (foo == null) {
    // comment
    foo = makeFoo();
  }`, suggested: `declare let foo: string | null;
declare function makeFoo(): string;

function lazyInitialize() {
  // comment
foo ??= makeFoo();
}
`},
	}},
	{name: "upstream invalid[339]", source: `
declare let foo: string | null;
declare function makeFoo(): string;

if (foo == null) {
  // comment before 1
  /* comment before 2 */
  /* comment before 3
    which is multiline
  */
  /**
   * comment before 4
   * which is also multiline
   */
  foo = makeFoo(); // comment inline
  // comment after 1
  /* comment after 2 */
  /* comment after 3
    which is multiline
  */
  /**
   * comment after 4
   * which is also multiline
   */
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 4, column: 1, reported: `if (foo == null) {
  // comment before 1
  /* comment before 2 */
  /* comment before 3
    which is multiline
  */
  /**
   * comment before 4
   * which is also multiline
   */
  foo = makeFoo(); // comment inline
  // comment after 1
  /* comment after 2 */
  /* comment after 3
    which is multiline
  */
  /**
   * comment after 4
   * which is also multiline
   */
}`, suggested: `declare let foo: string | null;
declare function makeFoo(): string;

// comment before 1
/* comment before 2 */
/* comment before 3
    which is multiline
  */
/**
   * comment before 4
   * which is also multiline
   */
foo ??= makeFoo(); // comment inline
// comment after 1
/* comment after 2 */
/* comment after 3
    which is multiline
  */
/**
   * comment after 4
   * which is also multiline
   */
`},
	}},
	{name: "upstream invalid[340]", source: `
declare let foo: string | null;
declare function makeFoo(): string;

if (foo == null) /* comment before 1 */ /* comment before 2 */ foo = makeFoo(); // comment inline
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverAssignment",
			line:     4,
			column:   1,
			reported: `if (foo == null) /* comment before 1 */ /* comment before 2 */ foo = makeFoo();`,
			suggested: `declare let foo: string | null;
declare function makeFoo(): string;

/* comment before 1 */ /* comment before 2 */ foo ??= makeFoo(); // comment inline
`,
		},
	}},
	{name: "upstream invalid[341]", source: `
declare let foo: { a: string | null };
declare function makeString(): string;

function weirdParens() {
  if (((((foo.a)) == null))) {
    ((((((((foo).a))))) = makeString()));
  }
}
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverAssignment", line: 5, column: 3, reported: `if (((((foo.a)) == null))) {
    ((((((((foo).a))))) = makeString()));
  }`, suggested: `declare let foo: { a: string | null };
declare function makeString(): string;

function weirdParens() {
  ((foo).a) ??= makeString();
}
`},
	}},
}
