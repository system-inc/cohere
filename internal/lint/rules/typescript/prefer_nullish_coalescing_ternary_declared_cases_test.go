package typescript

// preferNullishCoalescingTernaryDeclaredCases are the rest of upstream's invalid cases that report from the
// ternary path, from invalid[96], in upstream's order. Each source declares what it tests. Its last three,
// invalid[342] to [344], come after the if-statement cases upstream.
var preferNullishCoalescingTernaryDeclaredCases = []preferNullishCoalescingReplayedCase{
	{name: "upstream invalid[96]", source: `
declare let x: string | null | undefined;
x ? x : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x ? x : y`,
			suggested: `declare let x: string | null | undefined;
x ?? y;
`,
		},
	}},
	{name: "upstream invalid[97]", source: `
declare let x: string | null | undefined;
!x ? y : x;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x ? y : x`,
			suggested: `declare let x: string | null | undefined;
x ?? y;
`,
		},
	}},
	{name: "upstream invalid[98]", source: `
declare let x: number | null | undefined;
x ? x : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x ? x : y`,
			suggested: `declare let x: number | null | undefined;
x ?? y;
`,
		},
	}},
	{name: "upstream invalid[99]", source: `
declare let x: number | null | undefined;
!x ? y : x;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x ? y : x`,
			suggested: `declare let x: number | null | undefined;
x ?? y;
`,
		},
	}},
	{name: "upstream invalid[100]", source: `
declare let x: boolean | null | undefined;
x ? x : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x ? x : y`,
			suggested: `declare let x: boolean | null | undefined;
x ?? y;
`,
		},
	}},
	{name: "upstream invalid[101]", source: `
declare let x: boolean | null | undefined;
!x ? y : x;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x ? y : x`,
			suggested: `declare let x: boolean | null | undefined;
x ?? y;
`,
		},
	}},
	{name: "upstream invalid[102]", source: `
declare let x: object | null | undefined;
x ? x : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x ? x : y`,
			suggested: `declare let x: object | null | undefined;
x ?? y;
`,
		},
	}},
	{name: "upstream invalid[103]", source: `
declare let x: object | null | undefined;
!x ? y : x;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x ? y : x`,
			suggested: `declare let x: object | null | undefined;
x ?? y;
`,
		},
	}},
	{name: "upstream invalid[104]", source: `
declare let x: { n: string | null | undefined };
x.n ? x.n : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n ? x.n : y`,
			suggested: `declare let x: { n: string | null | undefined };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[105]", source: `
declare let x: { n: string | null | undefined };
!x.n ? y : x.n;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x.n ? y : x.n`,
			suggested: `declare let x: { n: string | null | undefined };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[106]", source: `
declare let x: { n: number | null | undefined };
x.n ? x.n : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n ? x.n : y`,
			suggested: `declare let x: { n: number | null | undefined };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[107]", source: `
declare let x: { n: number | null | undefined };
!x.n ? y : x.n;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x.n ? y : x.n`,
			suggested: `declare let x: { n: number | null | undefined };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[108]", source: `
declare let x: { n: boolean | null | undefined };
x.n ? x.n : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n ? x.n : y`,
			suggested: `declare let x: { n: boolean | null | undefined };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[109]", source: `
declare let x: { n: boolean | null | undefined };
!x.n ? y : x.n;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x.n ? y : x.n`,
			suggested: `declare let x: { n: boolean | null | undefined };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[110]", source: `
declare let x: { n: object | null | undefined };
x.n ? x.n : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n ? x.n : y`,
			suggested: `declare let x: { n: object | null | undefined };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[111]", source: `
declare let x: { n: object | null | undefined };
!x.n ? y : x.n;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x.n ? y : x.n`,
			suggested: `declare let x: { n: object | null | undefined };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[112]", source: `
declare let x: { n?: { a?: string } };
x.n?.a ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[113]", source: `
declare let x: { n?: { a?: string } };
x.n?.a ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[114]", source: `
declare let x: { n?: { a?: string } };
x.n?.a ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[115]", source: `
declare let x: { n?: { a?: string } };
x.n?.a !== undefined ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a !== undefined ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[116]", source: `
declare let x: { n?: { a?: string } };
x.n?.a !== undefined ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a !== undefined ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[117]", source: `
declare let x: { n?: { a?: string } };
x.n?.a !== undefined ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a !== undefined ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[118]", source: `
declare let x: { n?: { a?: string } };
x.n?.a != undefined ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a != undefined ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[119]", source: `
declare let x: { n?: { a?: string } };
x.n?.a != undefined ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a != undefined ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[120]", source: `
declare let x: { n?: { a?: string } };
x.n?.a != undefined ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a != undefined ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[121]", source: `
declare let x: { n?: { a?: string } };
x.n?.a != null ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a != null ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[122]", source: `
declare let x: { n?: { a?: string } };
x.n?.a != null ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a != null ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[123]", source: `
declare let x: { n?: { a?: string } };
x.n?.a != null ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a != null ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[124]", source: `
declare let x: { n?: { a?: string | null } };
x.n?.a !== undefined && x.n.a !== null ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a !== undefined && x.n.a !== null ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[125]", source: `
declare let x: { n?: { a?: string | null } };
x.n?.a !== undefined && x.n.a !== null ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a !== undefined && x.n.a !== null ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[126]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[127]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a ? x.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a ? x.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[128]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[129]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[130]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a !== undefined ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a !== undefined ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[131]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a !== undefined ? x.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a !== undefined ? x.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[132]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a !== undefined ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a !== undefined ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[133]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a !== undefined ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a !== undefined ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[134]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a != undefined ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a != undefined ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[135]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a != undefined ? x.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a != undefined ? x.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[136]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a != undefined ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a != undefined ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[137]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a != undefined ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a != undefined ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[138]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a != null ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a != null ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[139]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a != null ? x.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a != null ? x.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[140]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a != null ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a != null ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[141]", source: `
declare let x: { n?: { a?: string } };
x?.n?.a != null ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a != null ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[142]", source: `
declare let x: { n?: { a?: string | null } };
x?.n?.a !== undefined && x.n.a !== null ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a !== undefined && x.n.a !== null ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[143]", source: `
declare let x: { n?: { a?: string | null } };
x?.n?.a !== undefined && x.n.a !== null ? x.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a !== undefined && x.n.a !== null ? x.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[144]", source: `
declare let x: { n?: { a?: string | null } };
x?.n?.a !== undefined && x.n.a !== null ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a !== undefined && x.n.a !== null ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[145]", source: `
declare let x: { n?: { a?: string | null } };
x?.n?.a !== undefined && x.n.a !== null ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a !== undefined && x.n.a !== null ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[146]", source: `
declare let x: { n?: { a?: string | null } };
x?.n?.a ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[147]", source: `
declare let x: { n?: { a?: string | null } };
x?.n?.a ? x.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a ? x.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[148]", source: `
declare let x: { n?: { a?: string | null } };
x?.n?.a ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[149]", source: `
declare let x: { n?: { a?: string | null } };
x?.n?.a ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x?.n?.a ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x?.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[150]", source: `
declare let x: { n?: { a?: string | null } };
x.n?.a ? x?.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a ? x?.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[151]", source: `
declare let x: { n?: { a?: string | null } };
x.n?.a ? x.n?.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a ? x.n?.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[152]", source: `
declare let x: { n?: { a?: string | null } };
x.n?.a ? x?.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a ? x?.n.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[153]", source: `
declare let x: { n?: { a?: string | null } };
x.n?.a ? x.n.a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a ? x.n.a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x.n?.a ?? y;
`,
		},
	}},
	{name: "upstream invalid[154]", source: `
declare let x: { n?: { a?: string | null } };
x.n?.a ? (x?.n).a : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `x.n?.a ? (x?.n).a : y`,
			suggested: `declare let x: { n?: { a?: string | null } };
x.n?.a ?? y;
`,
		},
	}},
	{
		name: "upstream invalid[232]",
		source: `
declare let x: string | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: string | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[233]",
		source: `
declare let x: number | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: number | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[234]",
		source: `
declare let x: boolean | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: boolean | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[235]",
		source: `
declare let x: bigint | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"boolean\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: bigint | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[241]",
		source: `
declare let x: '' | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: '' | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name:        "upstream invalid[242]",
		source:      "\ndeclare let x: `` | undefined;\nx ? x : y;\n      ",
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:        "preferNullishOverTernary",
				line:      2,
				column:    1,
				reported:  `x ? x : y`,
				suggested: "declare let x: `` | undefined;\nx ?? y;\n",
			},
		},
	},
	{
		name: "upstream invalid[243]",
		source: `
declare let x: 0 | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 0 | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[244]",
		source: `
declare let x: 0n | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 0n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[245]",
		source: `
declare let x: false | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":false,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: false | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[251]",
		source: `
declare let x: 'a' | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 'a' | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[252]",
		source: `
declare let x: 'a' | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 'a' | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name:        "upstream invalid[253]",
		source:      "\ndeclare let x: `hello${'string'}` | undefined;\nx ? x : y;\n      ",
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:        "preferNullishOverTernary",
				line:      2,
				column:    1,
				reported:  `x ? x : y`,
				suggested: "declare let x: `hello${'string'}` | undefined;\nx ?? y;\n",
			},
		},
	},
	{
		name:        "upstream invalid[254]",
		source:      "\ndeclare let x: `hello${'string'}` | undefined;\n!x ? y : x;\n      ",
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:        "preferNullishOverTernary",
				line:      2,
				column:    1,
				reported:  `!x ? y : x`,
				suggested: "declare let x: `hello${'string'}` | undefined;\nx ?? y;\n",
			},
		},
	},
	{
		name: "upstream invalid[255]",
		source: `
declare let x: 1 | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 1 | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[256]",
		source: `
declare let x: 1 | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 1 | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[257]",
		source: `
declare let x: 1n | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 1n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[258]",
		source: `
declare let x: 1n | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 1n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[259]",
		source: `
declare let x: true | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":false,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: true | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[260]",
		source: `
declare let x: true | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":false,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: true | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[268]",
		source: `
declare let x: 'a' | 'b' | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 'a' | 'b' | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[269]",
		source: `
declare let x: 'a' | 'b' | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 'a' | 'b' | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name:        "upstream invalid[270]",
		source:      "\ndeclare let x: 'a' | `b` | undefined;\nx ? x : y;\n      ",
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:        "preferNullishOverTernary",
				line:      2,
				column:    1,
				reported:  `x ? x : y`,
				suggested: "declare let x: 'a' | `b` | undefined;\nx ?? y;\n",
			},
		},
	},
	{
		name:        "upstream invalid[271]",
		source:      "\ndeclare let x: 'a' | `b` | undefined;\n!x ? y : x;\n      ",
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":true,\"string\":false}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:        "preferNullishOverTernary",
				line:      2,
				column:    1,
				reported:  `!x ? y : x`,
				suggested: "declare let x: 'a' | `b` | undefined;\nx ?? y;\n",
			},
		},
	},
	{
		name: "upstream invalid[272]",
		source: `
declare let x: 0 | 1 | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 0 | 1 | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[273]",
		source: `
declare let x: 0 | 1 | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 0 | 1 | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[274]",
		source: `
declare let x: 1 | 2 | 3 | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 1 | 2 | 3 | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[275]",
		source: `
declare let x: 1 | 2 | 3 | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 1 | 2 | 3 | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[276]",
		source: `
declare let x: 0n | 1n | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 0n | 1n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[277]",
		source: `
declare let x: 0n | 1n | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 0n | 1n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[278]",
		source: `
declare let x: 1n | 2n | 3n | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 1n | 2n | 3n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[279]",
		source: `
declare let x: 1n | 2n | 3n | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 1n | 2n | 3n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[280]",
		source: `
declare let x: true | false | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":false,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: true | false | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[281]",
		source: `
declare let x: true | false | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":false,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: true | false | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[284]",
		source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: 0 | 1 | 0n | 1n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[285]",
		source: `
declare let x: 0 | 1 | 0n | 1n | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":false,\"boolean\":true,\"number\":false,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: 0 | 1 | 0n | 1n | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[286]",
		source: `
declare let x: true | false | null | undefined;
x ? x : y;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":false,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `x ? x : y`,
				suggested: `declare let x: true | false | null | undefined;
x ?? y;
`,
			},
		},
	},
	{
		name: "upstream invalid[287]",
		source: `
declare let x: true | false | null | undefined;
!x ? y : x;
      `,
		optionsJson: "{\"ignorePrimitives\":{\"bigint\":true,\"boolean\":false,\"number\":true,\"string\":true}}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     2,
				column:   1,
				reported: `!x ? y : x`,
				suggested: `declare let x: true | false | null | undefined;
x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[303]", source: `
let a: string | true | undefined;
let b: string | boolean | undefined;

const x = Boolean(a ? a : b);
      `, optionsJson: "{\"ignoreBooleanCoercion\":true}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     4,
			column:   19,
			reported: `a ? a : b`,
			suggested: `let a: string | true | undefined;
let b: string | boolean | undefined;

const x = Boolean(a ?? b);
`,
		},
	}},
	{name: "upstream invalid[304]", source: `
let a: string | boolean | undefined;
let b: string | boolean | undefined;

const test = Boolean(!a ? b : a);
      `, optionsJson: "{\"ignoreBooleanCoercion\":true}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     4,
			column:   22,
			reported: `!a ? b : a`,
			suggested: `let a: string | boolean | undefined;
let b: string | boolean | undefined;

const test = Boolean(a ?? b);
`,
		},
	}},
	{name: "upstream invalid[308]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBox: Box | undefined;

defaultBox ? defaultBox : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     7,
			column:   1,
			reported: `defaultBox ? defaultBox : getFallbackBox()`,
			suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBox: Box | undefined;

defaultBox ?? getFallbackBox();
`,
		},
	}},
	{name: "upstream invalid[309]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b != null ? defaultBoxOptional.a?.b : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     7,
			column:   1,
			reported: `defaultBoxOptional.a?.b != null ? defaultBoxOptional.a?.b : getFallbackBox()`,
			suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ?? getFallbackBox();
`,
		},
	}},
	{name: "upstream invalid[312]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b != null ? defaultBoxOptional.a.b : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     7,
			column:   1,
			reported: `defaultBoxOptional.a?.b != null ? defaultBoxOptional.a.b : getFallbackBox()`,
			suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ?? getFallbackBox();
`,
		},
	}},
	{name: "upstream invalid[313]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ? defaultBoxOptional.a?.b : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     7,
			column:   1,
			reported: `defaultBoxOptional.a?.b ? defaultBoxOptional.a?.b : getFallbackBox()`,
			suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ?? getFallbackBox();
`,
		},
	}},
	{name: "upstream invalid[314]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ? defaultBoxOptional.a.b : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     7,
			column:   1,
			reported: `defaultBoxOptional.a?.b ? defaultBoxOptional.a.b : getFallbackBox()`,
			suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ?? getFallbackBox();
`,
		},
	}},
	{name: "upstream invalid[315]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b !== undefined
  ? defaultBoxOptional.a?.b
  : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 7, column: 1, reported: `defaultBoxOptional.a?.b !== undefined
  ? defaultBoxOptional.a?.b
  : getFallbackBox()`, suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ?? getFallbackBox();
`},
	}},
	{name: "upstream invalid[316]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b !== undefined
  ? defaultBoxOptional.a.b
  : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 7, column: 1, reported: `defaultBoxOptional.a?.b !== undefined
  ? defaultBoxOptional.a.b
  : getFallbackBox()`, suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ?? getFallbackBox();
`},
	}},
	{name: "upstream invalid[317]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b !== undefined && defaultBoxOptional.a?.b !== null
  ? defaultBoxOptional.a?.b
  : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   7,
			column: 1,
			reported: `defaultBoxOptional.a?.b !== undefined && defaultBoxOptional.a?.b !== null
  ? defaultBoxOptional.a?.b
  : getFallbackBox()`,
			suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ?? getFallbackBox();
`,
		},
	}},
	{name: "upstream invalid[318]", source: `
interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b !== undefined && defaultBoxOptional.a?.b !== null
  ? defaultBoxOptional.a.b
  : getFallbackBox();
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   7,
			column: 1,
			reported: `defaultBoxOptional.a?.b !== undefined && defaultBoxOptional.a?.b !== null
  ? defaultBoxOptional.a.b
  : getFallbackBox()`,
			suggested: `interface Box {
  value: string;
}
declare function getFallbackBox(): Box;
declare const defaultBoxOptional: { a?: { b?: Box | undefined } };

defaultBoxOptional.a?.b ?? getFallbackBox();
`,
		},
	}},
	{name: "upstream invalid[319]", source: `
declare let x: unknown;
declare let y: number;
!x ? y : x;
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 3, column: 1, reported: `!x ? y : x`, suggested: `declare let x: unknown;
declare let y: number;
x ?? y;
`},
	}},
	{name: "upstream invalid[320]", source: `
declare let x: unknown;
declare let y: number;
x ? x : y;
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 3, column: 1, reported: `x ? x : y`, suggested: `declare let x: unknown;
declare let y: number;
x ?? y;
`},
	}},
	{name: "upstream invalid[321]", source: `
declare let x: { n: unknown };
!x.n ? y : x.n;
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `!x.n ? y : x.n`,
			suggested: `declare let x: { n: unknown };
x.n ?? y;
`,
		},
	}},
	{name: "upstream invalid[322]", source: `
declare let x: { a: string } | null;

x?.['a'] != null ? x['a'] : 'foo';
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     3,
			column:   1,
			reported: `x?.['a'] != null ? x['a'] : 'foo'`,
			suggested: `declare let x: { a: string } | null;

x?.['a'] ?? 'foo';
`,
		},
	}},
	{name: "upstream invalid[323]", source: `
declare let x: { a: string } | null;

x?.['a'] != null ? x.a : 'foo';
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     3,
			column:   1,
			reported: `x?.['a'] != null ? x.a : 'foo'`,
			suggested: `declare let x: { a: string } | null;

x?.['a'] ?? 'foo';
`,
		},
	}},
	{name: "upstream invalid[324]", source: `
declare let x: { a: string } | null;

x?.a != null ? x['a'] : 'foo';
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     3,
			column:   1,
			reported: `x?.a != null ? x['a'] : 'foo'`,
			suggested: `declare let x: { a: string } | null;

x?.a ?? 'foo';
`,
		},
	}},
	{name: "upstream invalid[325]", source: `
const a = 'b';
declare let x: { a: string; b: string } | null;

x?.[a] != null ? x[a] : 'foo';
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     4,
			column:   1,
			reported: `x?.[a] != null ? x[a] : 'foo'`,
			suggested: `const a = 'b';
declare let x: { a: string; b: string } | null;

x?.[a] ?? 'foo';
`,
		},
	}},
	{name: "upstream invalid[342]", source: `
let a: string | undefined;
let b: { message: string } | undefined;

const foo = a ? a : b ? 1 : 2;
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     4,
			column:   13,
			reported: `a ? a : b ? 1 : 2`,
			suggested: `let a: string | undefined;
let b: { message: string } | undefined;

const foo = a ?? (b ? 1 : 2);
`,
		},
	}},
	{name: "upstream invalid[343]", source: `
let a: string | undefined;
let b: { message: string } | undefined;

const foo = a ? a : (b ? 1 : 2);
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     4,
			column:   13,
			reported: `a ? a : (b ? 1 : 2)`,
			suggested: `let a: string | undefined;
let b: { message: string } | undefined;

const foo = a ?? (b ? 1 : 2);
`,
		},
	}},
	{name: "upstream invalid[344]", source: `
declare const c: string | null;
c !== null ? c : c ? 1 : 2;
      `, optionsJson: "", findings: []preferNullishCoalescingReplayed{
		{
			id:       "preferNullishOverTernary",
			line:     2,
			column:   1,
			reported: `c !== null ? c : c ? 1 : 2`,
			suggested: `declare const c: string | null;
c ?? (c ? 1 : 2);
`,
		},
	}},
}
