package typescript

// preferNullishCoalescingTernaryComparisonCases are upstream's invalid[8] to [95], the first of its invalid cases
// that report from the ternary path, in upstream's order. Each compares its subject with null or undefined and
// declares nothing. preferNullishCoalescingTernaryDeclaredCases carries on from invalid[96].
var preferNullishCoalescingTernaryComparisonCases = []preferNullishCoalescingReplayedCase{
	{
		name:        "upstream invalid[8]",
		source:      `x !== undefined && x !== null ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== undefined && x !== null ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[9]", source: `
x.z[1][this[this.o]]['3'][a.b.c] !== undefined &&
x.z[1][this[this.o]]['3'][a.b.c] !== null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `x.z[1][this[this.o]]['3'][a.b.c] !== undefined &&
x.z[1][this[this.o]]['3'][a.b.c] !== null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[10]",
		source:      `x !== undefined && x !== null ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== undefined && x !== null ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[11]",
		source:      `x !== null && x !== undefined ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== null && x !== undefined ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[12]", source: `
x.z[1][this[this.o]]['3'][a.b.c] !== null &&
x.z[1][this[this.o]]['3'][a.b.c] !== undefined
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] !== null &&
x.z[1][this[this.o]]['3'][a.b.c] !== undefined
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[13]",
		source:      `x !== null && x !== undefined ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== null && x !== undefined ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[14]",
		source:      `x === undefined || x === null ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x === undefined || x === null ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[15]", source: `
x.z[1][this[this.o]]['3'][a.b.c] === undefined ||
x.z[1][this[this.o]]['3'][a.b.c] === null
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `x.z[1][this[this.o]]['3'][a.b.c] === undefined ||
x.z[1][this[this.o]]['3'][a.b.c] === null
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[16]",
		source:      `x === undefined || x === null ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x === undefined || x === null ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[17]",
		source:      `x === null || x === undefined ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x === null || x === undefined ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[18]", source: `
x.z[1][this[this.o]]['3'][a.b.c] === null ||
x.z[1][this[this.o]]['3'][a.b.c] === undefined
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] === null ||
x.z[1][this[this.o]]['3'][a.b.c] === undefined
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[19]",
		source:      `x === null || x === undefined ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x === null || x === undefined ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[20]",
		source:      `undefined !== x && x !== null ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined !== x && x !== null ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[21]", source: `
undefined !== x.z[1][this[this.o]]['3'][a.b.c] &&
x.z[1][this[this.o]]['3'][a.b.c] !== null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `undefined !== x.z[1][this[this.o]]['3'][a.b.c] &&
x.z[1][this[this.o]]['3'][a.b.c] !== null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[22]",
		source:      `undefined !== x && x !== null ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined !== x && x !== null ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[23]",
		source:      `null !== x && x !== undefined ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null !== x && x !== undefined ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[24]", source: `
null !== x.z[1][this[this.o]]['3'][a.b.c] &&
x.z[1][this[this.o]]['3'][a.b.c] !== undefined
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `null !== x.z[1][this[this.o]]['3'][a.b.c] &&
x.z[1][this[this.o]]['3'][a.b.c] !== undefined
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[25]",
		source:      `null !== x && x !== undefined ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null !== x && x !== undefined ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[26]",
		source:      `undefined === x || x === null ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined === x || x === null ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[27]", source: `
undefined === x.z[1][this[this.o]]['3'][a.b.c] ||
x.z[1][this[this.o]]['3'][a.b.c] === null
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `undefined === x.z[1][this[this.o]]['3'][a.b.c] ||
x.z[1][this[this.o]]['3'][a.b.c] === null
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[28]",
		source:      `undefined === x || x === null ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined === x || x === null ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[29]",
		source:      `null === x || x === undefined ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null === x || x === undefined ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[30]", source: `
null === x.z[1][this[this.o]]['3'][a.b.c] ||
x.z[1][this[this.o]]['3'][a.b.c] === undefined
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `null === x.z[1][this[this.o]]['3'][a.b.c] ||
x.z[1][this[this.o]]['3'][a.b.c] === undefined
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[31]",
		source:      `null === x || x === undefined ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null === x || x === undefined ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[32]",
		source:      `x !== undefined && null !== x ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== undefined && null !== x ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[33]", source: `
x.z[1][this[this.o]]['3'][a.b.c] !== undefined &&
null !== x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `x.z[1][this[this.o]]['3'][a.b.c] !== undefined &&
null !== x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[34]",
		source:      `x !== undefined && null !== x ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== undefined && null !== x ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[35]",
		source:      `x !== null && undefined !== x ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== null && undefined !== x ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[36]", source: `
x.z[1][this[this.o]]['3'][a.b.c] !== null &&
undefined !== x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] !== null &&
undefined !== x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[37]",
		source:      `x !== null && undefined !== x ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== null && undefined !== x ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[38]",
		source:      `x === undefined || null === x ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x === undefined || null === x ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[39]", source: `
x.z[1][this[this.o]]['3'][a.b.c] === undefined ||
null === x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `x.z[1][this[this.o]]['3'][a.b.c] === undefined ||
null === x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[40]",
		source:      `x === undefined || null === x ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x === undefined || null === x ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[41]",
		source:      `x === null || undefined === x ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x === null || undefined === x ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[42]", source: `
x.z[1][this[this.o]]['3'][a.b.c] === null ||
undefined === x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] === null ||
undefined === x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[43]",
		source:      `x === null || undefined === x ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x === null || undefined === x ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[44]",
		source:      `undefined !== x && null !== x ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined !== x && null !== x ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[45]", source: `
undefined !== x.z[1][this[this.o]]['3'][a.b.c] &&
null !== x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `undefined !== x.z[1][this[this.o]]['3'][a.b.c] &&
null !== x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[46]",
		source:      `undefined !== x && null !== x ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined !== x && null !== x ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[47]",
		source:      `null !== x && undefined !== x ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null !== x && undefined !== x ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[48]", source: `
null !== x.z[1][this[this.o]]['3'][a.b.c] &&
undefined !== x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `null !== x.z[1][this[this.o]]['3'][a.b.c] &&
undefined !== x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[49]",
		source:      `null !== x && undefined !== x ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null !== x && undefined !== x ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[50]",
		source:      `undefined === x || null === x ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined === x || null === x ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[51]", source: `
undefined === x.z[1][this[this.o]]['3'][a.b.c] ||
null === x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `undefined === x.z[1][this[this.o]]['3'][a.b.c] ||
null === x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[52]",
		source:      `undefined === x || null === x ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined === x || null === x ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[53]",
		source:      `null === x || undefined === x ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null === x || undefined === x ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[54]", source: `
null === x.z[1][this[this.o]]['3'][a.b.c] ||
undefined === x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `null === x.z[1][this[this.o]]['3'][a.b.c] ||
undefined === x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[55]",
		source:      `null === x || undefined === x ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null === x || undefined === x ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[56]",
		source:      `x != undefined && x != null ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x != undefined && x != null ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[57]", source: `
x.z[1][this[this.o]]['3'][a.b.c] != undefined &&
x.z[1][this[this.o]]['3'][a.b.c] != null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] != undefined &&
x.z[1][this[this.o]]['3'][a.b.c] != null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[58]",
		source:      `x != undefined && x != null ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x != undefined && x != null ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[59]",
		source:      `x == undefined || x == null ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x == undefined || x == null ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[60]", source: `
x.z[1][this[this.o]]['3'][a.b.c] == undefined ||
x.z[1][this[this.o]]['3'][a.b.c] == null
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] == undefined ||
x.z[1][this[this.o]]['3'][a.b.c] == null
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[61]",
		source:      `x == undefined || x == null ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x == undefined || x == null ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[62]",
		source:      `x != undefined && x !== null ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x != undefined && x !== null ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[63]", source: `
x.z[1][this[this.o]]['3'][a.b.c] != undefined &&
x.z[1][this[this.o]]['3'][a.b.c] !== null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] != undefined &&
x.z[1][this[this.o]]['3'][a.b.c] !== null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[64]",
		source:      `x != undefined && x !== null ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x != undefined && x !== null ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[65]",
		source:      `x == undefined || x === null ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x == undefined || x === null ? y : x`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[66]", source: `
x.z[1][this[this.o]]['3'][a.b.c] == undefined ||
x.z[1][this[this.o]]['3'][a.b.c] === null
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] == undefined ||
x.z[1][this[this.o]]['3'][a.b.c] === null
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[67]",
		source:      `x == undefined || x === null ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x == undefined || x === null ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[68]",
		source:      `x !== undefined && x != null ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== undefined && x != null ? x : y`,
				suggested: `x ?? y;
`,
			},
		},
	},
	{name: "upstream invalid[69]", source: `
x.z[1][this[this.o]]['3'][a.b.c] !== undefined &&
x.z[1][this[this.o]]['3'][a.b.c] != null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{
			id:     "preferNullishOverTernary",
			line:   1,
			column: 1,
			reported: `x.z[1][this[this.o]]['3'][a.b.c] !== undefined &&
x.z[1][this[this.o]]['3'][a.b.c] != null
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`,
			suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
		},
	}},
	{
		name:        "upstream invalid[70]",
		source:      `x !== undefined && x != null ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x !== undefined && x != null ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[71]",
		source:      `undefined != x ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{id: "preferNullishOverTernary", line: 1, column: 1, reported: `undefined != x ? x : y`, suggested: `x ?? y;
`},
		},
	},
	{name: "upstream invalid[72]", source: `
undefined != x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `undefined != x.z[1][this[this.o]]['3'][a.b.c]
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[73]",
		source:      `undefined != x ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined != x ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[74]",
		source:      `null != x ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{id: "preferNullishOverTernary", line: 1, column: 1, reported: `null != x ? x : y`, suggested: `x ?? y;
`},
		},
	},
	{
		name:        "upstream invalid[75]",
		source:      `null != x.z[1][this[this.o]]['3'][a.b.c] ? x.z[1][this[this.o]]['3'][a.b.c] : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null != x.z[1][this[this.o]]['3'][a.b.c] ? x.z[1][this[this.o]]['3'][a.b.c] : y`,
				suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
			},
		},
	},
	{
		name:        "upstream invalid[76]",
		source:      `null != x ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null != x ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[77]",
		source:      `undefined == x ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{id: "preferNullishOverTernary", line: 1, column: 1, reported: `undefined == x ? y : x`, suggested: `x ?? y;
`},
		},
	},
	{name: "upstream invalid[78]", source: `
undefined == x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `undefined == x.z[1][this[this.o]]['3'][a.b.c]
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[79]",
		source:      `undefined == x ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `undefined == x ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[80]",
		source:      `null == x ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{id: "preferNullishOverTernary", line: 1, column: 1, reported: `null == x ? y : x`, suggested: `x ?? y;
`},
		},
	},
	{
		name:        "upstream invalid[81]",
		source:      `null == x.z[1][this[this.o]]['3'][a.b.c] ? y : x.z[1][this[this.o]]['3'][a.b.c];`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null == x.z[1][this[this.o]]['3'][a.b.c] ? y : x.z[1][this[this.o]]['3'][a.b.c]`,
				suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
			},
		},
	},
	{
		name:        "upstream invalid[82]",
		source:      `null == x ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `null == x ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[83]",
		source:      `x != undefined ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x != undefined ? x : y`, suggested: `x ?? y;
`},
		},
	},
	{name: "upstream invalid[84]", source: `
x.z[1][this[this.o]]['3'][a.b.c] != undefined
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y;
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] != undefined
  ? x.z[1][this[this.o]]['3'][a.b.c]
  : y`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[85]",
		source:      `x != undefined ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x != undefined ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[86]",
		source:      `x != null ? x : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x != null ? x : y`, suggested: `x ?? y;
`},
		},
	},
	{
		name:        "upstream invalid[87]",
		source:      `x.z[1][this[this.o]]['3'][a.b.c] != null ? x.z[1][this[this.o]]['3'][a.b.c] : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x.z[1][this[this.o]]['3'][a.b.c] != null ? x.z[1][this[this.o]]['3'][a.b.c] : y`,
				suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
			},
		},
	},
	{
		name:        "upstream invalid[88]",
		source:      `x != null ? x : (z = y);`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x != null ? x : (z = y)`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[89]",
		source:      `x == undefined ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x == undefined ? y : x`, suggested: `x ?? y;
`},
		},
	},
	{name: "upstream invalid[90]", source: `
x.z[1][this[this.o]]['3'][a.b.c] == undefined
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c];
      `, optionsJson: "{\"ignoreTernaryTests\":false}", findings: []preferNullishCoalescingReplayed{
		{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x.z[1][this[this.o]]['3'][a.b.c] == undefined
  ? y
  : x.z[1][this[this.o]]['3'][a.b.c]`, suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`},
	}},
	{
		name:        "upstream invalid[91]",
		source:      `x == undefined ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x == undefined ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[92]",
		source:      `x == null ? y : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{id: "preferNullishOverTernary", line: 1, column: 1, reported: `x == null ? y : x`, suggested: `x ?? y;
`},
		},
	},
	{
		name:        "upstream invalid[93]",
		source:      `x.z[1][this[this.o]]['3'][a.b.c] == null ? y : x.z[1][this[this.o]]['3'][a.b.c];`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x.z[1][this[this.o]]['3'][a.b.c] == null ? y : x.z[1][this[this.o]]['3'][a.b.c]`,
				suggested: `x.z[1][this[this.o]]['3'][a.b.c] ?? y;
`,
			},
		},
	},
	{
		name:        "upstream invalid[94]",
		source:      `x == null ? (z = y) : x;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `x == null ? (z = y) : x`,
				suggested: `x ?? (z = y);
`,
			},
		},
	},
	{
		name:        "upstream invalid[95]",
		source:      `this != undefined ? this : y;`,
		optionsJson: "{\"ignoreTernaryTests\":false}",
		findings: []preferNullishCoalescingReplayed{
			{
				id:       "preferNullishOverTernary",
				line:     1,
				column:   1,
				reported: `this != undefined ? this : y`,
				suggested: `this ?? y;
`,
			},
		},
	},
}
