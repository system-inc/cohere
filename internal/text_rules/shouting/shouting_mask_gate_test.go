package shouting

import (
	"slices"
	"strings"
	"testing"
)

// gateFixture is one comment the gates are compared over, written to hold the shapes the masks exist for.
//
// These replace a sample of real comments read from another repository. A sample can only show the shapes it
// happens to hold, and twice now the shape a gate broke was one no file in the sample held, so each shape a mask
// handles is written here on purpose: an acronym, code in backticks, an SQL keyword bare and quoted, an identifier in
// capitals, a fence, an indented block, a JSDoc example, and each command starter.
type gateFixture struct {
	name string
	text string

	// commandStarter, when set, is the one entry of commandStarters, or the `s\s+c` arm, that admits the text to
	// the command gate. The text holds no other starter, so a starter dropped from the gate is a fixture whose
	// gated mask differs from the ungated one, rather than one another starter still lets through.
	commandStarter string

	// wantShouts is what a reader is shown for the whole text once every mask has run. Each fixture names the
	// shouts it holds outside code, so a fixture that stopped reaching the token scan cannot pass by comparing
	// nothing with nothing.
	wantShouts []string
}

// structureStarter names the `s\s+c` arm in gateFixture.commandStarter, which no entry of commandStarters spells.
const structureStarter = `s\s+c`

var gateFixtures = []gateFixture{
	{
		name: "a JSDoc example followed by another tag",
		text: "/**\n" +
			" * Formats an hour for a clock face.\n" +
			" *\n" +
			" * @example\n" +
			" * formatHour(0) // 'AM' and NEVER noon\n" +
			" * formatHour(13) // returns PM\n" +
			" * @param hour The hour from 0 to 23, read in UTC.\n" +
			" * @returns The label, which is NOT localized.\n" +
			" */",
		wantShouts: []string{"NOT"},
	},
	{
		name:       "SQL in backticks beside a shout",
		text:       "// Reads `SELECT * FROM users WHERE id = ?` and NEVER the whole table.",
		wantShouts: []string{"NEVER"},
	},
	{
		name:       "SQL keywords left bare",
		text:       "// Rows come back ORDER BY created_at DESC, LIMIT 50.",
		wantShouts: []string{"ORDER", "BY", "DESC", "LIMIT"},
	},
	{
		name:       "SQL phrases in quotes",
		text:       "// The migration adds 'NOT NULL' and \"ON CONFLICT DO NOTHING\" to the column.",
		wantShouts: nil,
	},
	{
		name:       "identifiers in capitals",
		text:       "// MAX_RETRY_COUNT and DATABASE_URL come from the environment; HTTP2 and H3 are protocols.",
		wantShouts: nil,
	},
	{
		name:       "acronyms",
		text:       "// The API answers JSON over HTTP, and the CLI prints it as CSV behind the CDN.",
		wantShouts: nil,
	},
	{
		// Two capitals are the least the gate admits, so a gate that waited for a third would pass every fixture whose
		// short shout sits beside a longer one. This one has nothing else.
		name:       "a two-letter shout alone",
		text:       "// It IS the only copy, so it is kept.",
		wantShouts: []string{"IS"},
	},
	{
		name:       "a run joined by hyphens",
		text:       "// Per RFC-7231 and F-PROMISE-13 this is LOAD-BEARING.",
		wantShouts: []string{"LOAD", "BEARING"},
	},
	{
		name: "a fenced block",
		text: "/**\n" +
			" * Writes the row:\n" +
			" *\n" +
			" * ```sql\n" +
			" * INSERT INTO events VALUES (1, 'START');\n" +
			" * ```\n" +
			" *\n" +
			" * It MUST run inside a transaction.\n" +
			" */",
		wantShouts: []string{"MUST"},
	},
	{
		name: "an indented block",
		text: "/*\n" +
			" * The environment it reads:\n" +
			" *\n" +
			" *     HOME=/tmp TMPDIR=/tmp/scratch PATH=/usr/bin\n" +
			" *\n" +
			" * DO NOT export these.\n" +
			" */",
		wantShouts: []string{"DO", "NOT"},
	},
	{
		name:           "a shell prompt",
		text:           "/*\n * Rebuild everything first:\n *\n * $ make NOTHING\n */",
		commandStarter: "$",
	},
	{
		name:           "a git command",
		text:           "/*\n * Look before landing:\n *\n * git log --oneline NEVER\n */",
		commandStarter: "git",
	},
	{
		name:           "an npm command",
		text:           "/*\n * Then:\n *\n * npm run REBUILD\n */",
		commandStarter: "npm",
	},
	{
		name:           "an sqlite3 command",
		text:           "/*\n * Open the file:\n *\n * sqlite3 app.db VACUUM\n */",
		commandStarter: "sqlite3",
	},
	{
		name:           "a curl command",
		text:           "/*\n * Probe the health route:\n *\n * curl -X POST http://localhost:8080/RELOAD\n */",
		commandStarter: "curl",
	},
	{
		name:           "an ahra command",
		text:           "/*\n * Render them:\n *\n * ahra os skills render NOW\n */",
		commandStarter: "ahra",
	},
	{
		name:           "the structure command with two spaces",
		text:           "/*\n * Watch the tree:\n *\n * s  c watch NOW\n */",
		commandStarter: structureStarter,
	},
	{
		name:           "the structure command with a tab",
		text:           "/*\n * Watch the tree:\n *\n * s\tc watch NOW\n */",
		commandStarter: structureStarter,
	},
}

// gateFixtureTexts is every fixture's text, for the scans that compare a hand scan with its pattern over real
// comment shapes as well as generated strings.
func gateFixtureTexts() []string {
	texts := make([]string, 0, len(gateFixtures))
	for _, fixture := range gateFixtures {
		texts = append(texts, fixture.text)
	}
	return texts
}

// commandStartersIn is every command starter the gate would find in text, the `s\s+c` arm included.
func commandStartersIn(text string) []string {
	var found []string
	for _, starter := range commandStarters {
		if strings.Contains(text, starter) {
			found = append(found, starter)
		}
	}
	if structureCommandStarter.MatchString(text) {
		found = append(found, structureStarter)
	}
	return found
}

// referenceJsDocExamples is maskJsDocExamples as it was before its gate, kept apart so the comparison shares no code
// with the pass it checks.
func referenceJsDocExamples(text string) string {
	lines := strings.Split(text, "\n")
	insideExample := false
	for index, line := range lines {
		withoutGutter := jsDocGutter.ReplaceAllString(line, "")
		if exampleTag.MatchString(withoutGutter) {
			insideExample = true
			lines[index] = strings.Repeat(" ", len(line))
			continue
		}
		if insideExample && anyJsDocTag.MatchString(withoutGutter) {
			insideExample = false
		}
		if insideExample {
			lines[index] = strings.Repeat(" ", len(line))
		}
	}
	return strings.Join(lines, "\n")
}

// The gates must not change what masking produces, only how fast it gets there. The command pass is compared with
// blankCommandLines, the line pass the gate only decides whether to run, and the example pass with its form before
// the gate.
func TestShoutingMaskGatesPreserveOutput(test *testing.T) {
	test.Parallel()
	for _, fixture := range gateFixtures {
		test.Run(fixture.name, func(test *testing.T) {
			test.Parallel()
			if gated, reference := maskCommandLines(fixture.text), blankCommandLines(fixture.text); gated != reference {
				test.Errorf("the command gate changed the output:\ngated     %q\nreference %q", gated, reference)
			}
			gated, reference := maskJsDocExamples(fixture.text), referenceJsDocExamples(fixture.text)
			if gated != reference {
				test.Errorf("the example gate changed the output:\ngated     %q\nreference %q", gated, reference)
			}
			if fixture.commandStarter == "" {
				return
			}
			// A command fixture that the line pass leaves alone would agree with a gate that rejected it, so the
			// fixture proves nothing unless its command is actually masked and only its own starter admits it.
			if blankCommandLines(fixture.text) == fixture.text {
				test.Errorf("the line pass masks nothing in %q, so the gate's answer on it cannot be seen",
					fixture.text)
			}
			if found := commandStartersIn(fixture.text); !slices.Equal(found, []string{fixture.commandStarter}) {
				test.Errorf("the gate finds the starters %q in the fixture, want %q alone", found,
					fixture.commandStarter)
			}
		})
	}
}

// The uppercase gate must not change which tokens are reported, only how fast the answer arrives.
//
// Compared token by token rather than by count, for the same reason the comment guard is: a gate that lost one shout
// and gained another would hold a count stable while silencing a real finding. Each fixture is read whole and line by
// line, since a line alone loses the fence or the example tag that masks it, and the gate sees either.
func TestShoutingUppercaseGatePreservesTokens(test *testing.T) {
	test.Parallel()

	ungated := func(body string) []string {
		masked := MaskCodeAndCommands(body)
		var tokens []string
		seen := map[string]bool{}
		for _, span := range referenceUppercaseTokens(masked) {
			token := masked[span[0]:span[1]]
			if seen[token] || !isShoutedToken(token) {
				continue
			}
			seen[token] = true
			tokens = append(tokens, token)
		}
		return tokens
	}

	// The gate as every language applies it: checked on the text before masking.
	gatedTokens := func(body string) []string {
		if !HasAdjacentUppercaseLetters(body) {
			return nil
		}
		return distinctShoutedWords(MaskCodeAndCommands(body))
	}

	for _, fixture := range gateFixtures {
		test.Run(fixture.name, func(test *testing.T) {
			test.Parallel()
			if got := gatedTokens(fixture.text); !slices.Equal(got, fixture.wantShouts) {
				test.Errorf("the gated scan shows %q, want %q", got, fixture.wantShouts)
			}
			bodies := []string{fixture.text}
			for _, line := range strings.Split(fixture.text, "\n") {
				bodies = append(bodies, strings.TrimSpace(line))
			}
			for _, body := range bodies {
				if gated, reference := gatedTokens(body), ungated(body); !slices.Equal(gated, reference) {
					test.Errorf("the gate changed the tokens of %q: %q against %q", body, gated, reference)
				}
			}
		})
	}
}

// The gates must actually skip work, or they are pure overhead.
func TestShoutingMaskGatesActuallySkip(test *testing.T) {
	test.Parallel()

	plain := "// just some ordinary prose about the thing\n// with no commands and no tags\n"
	if containsAnyCommandStarter(plain) {
		test.Fatalf("command gate admits plain prose")
	}
	if strings.ContainsRune(plain, '@') {
		test.Fatalf("example gate admits plain prose")
	}

	// And they must admit the real cases, or the rule goes quiet for them.
	for _, admitted := range []string{
		"$ ls",
		"git commit",
		"pnpm install",
		"curl x",
		"s c",
		"ahra os",
		"sqlite3 db",
		"npm run",
	} {
		if !containsAnyCommandStarter(admitted) {
			test.Fatalf("command gate rejects %q, which the pattern matches", admitted)
		}
	}
	// The whitespace arm is the one a literal cannot express, and a fixture is the only thing that
	// catches it: 800 real files once contained no multi-space form, so a comparison over them passed
	// while the gate was wrong.
	for _, spacing := range []string{"s c", "s  c", "s\tc"} {
		if !containsAnyCommandStarter(spacing) {
			test.Fatalf("command gate rejects %q, which the pattern matches", spacing)
		}
	}
}

// The cases a sample of real comments cannot be relied on to hold, written out because that lesson has now cost
// twice.
func TestShoutingUppercaseGateAdmitsRealShouts(test *testing.T) {
	test.Parallel()

	// Every one of these must reach the expensive path.
	admitted := []string{
		"NEVER do this", "this is REALLY bad", "DO NOT EDIT",
		"a TODO marker", "the API contract", // acronyms: admitted, then declined downstream
		"MAX_RETRY_COUNT is an identifier", // underscore token
		"shouting at the very ENDOFLINE",
	}
	for _, body := range admitted {
		if !HasAdjacentUppercaseLetters(body) {
			test.Fatalf("gate rejects %q, which reaches the token scan", body)
		}
	}

	// And these must be skipped, or the gate is not a gate.
	skipped := []string{
		"ordinary prose about a thing",
		"a sentence with One capital",
		"CamelCase identifiers do not shout",
		"",
	}
	for _, body := range skipped {
		if HasAdjacentUppercaseLetters(body) {
			test.Fatalf("gate admits %q, which holds no adjacent uppercase pair", body)
		}
	}

	// The boundary: exactly two adjacent uppercase letters is the shortest reportable shape.
	if !HasAdjacentUppercaseLetters("it IS wrong") {
		test.Fatalf("gate rejects a two-letter shout")
	}
}
