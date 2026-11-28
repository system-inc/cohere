# `nexus/security-no-interpolated-shell-command`

|                        |                                                                                                                                                         |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Recommendation**     | **Yes**, at `error` once ahra's 25 findings are moved to `execFileSync`/`spawn` with an argument list. Registered `off` in ahra until then              |
| Findings               | **ahra 25, www-phi-health 0, www-connected-app 0, api-phi-health 0** (measured 2026-10-02)                                                              |
| Measured precision     | 25 of 25 true: in every finding a value the types do not prove literal reaches a shell parser. 6 are user-visible or latent bugs, 19 are harmless today |
| Plugin                 | cohere-native, `nexus`                                                                                                                                  |
| Auto-fixable           | no                                                                                                                                                      |
| Needs type information | yes, for the callee and for every value's literal-ness; without a checker the rule registers nothing                                                    |

## What it checks

A shell command string built at the call (a template, a `+` concatenation, a conditional, or a `const`
holding one) and handed to `node:child_process`'s `exec`/`execSync`, or to `spawn`, `spawnSync`,
`execFile` or `execFileSync` with a `shell` option typed `true` or a non-empty string, where a value
spliced into it is not typed as a string literal, number, bigint, boolean, enum member, `null` or
`undefined`. The callee is resolved by the checker's signature, so a promisified `exec` is seen and a
local `exec` is not. Values in the body of a quoted heredoc (`<<'EOSQL'`) are data to the shell and are
skipped. The rule's doc comment carries the precise version.

## Where it came from

A contact search that builds `LIKE '%${term}%'` from the searched words and hands it to the shell as
`` `sqlite3 "${file}" "${sql}"` ``. An apostrophe in a name breaks the SQL and the catch reports no
matches, and `$(...)` in a name that arrived from outside runs. Found by both research passes of
`#tevhg3f`, built as `#ednac8q`.

## Counts against the research

ahra has 37 `exec`/`execSync` calls cohere resolves (a categorizing probe of this rule, on the same run):
25 reported, 4 built but safe, 6 all-literal, 2 opaque. No `exec` call is promisified today, and the one
`shell: true` call has only literal arguments.

- **Cross-language pass, 31 dynamic + 6 literal + 1 `shell: true`:** exact match. 31 = 25 reported +
  4 safe + 2 opaque; the 6 literal and the `shell: true` call are silent.
- **JS pass, 20 string-typed + 8 non-literal + 3 literal-typed + 1 `shell:`:** the 20 direct templates
  are exactly cohere's 20 direct-template findings. Of its 8 non-literal commands, cohere reports 5,
  because it follows `const` bindings and `+`: four `const` templates and one concatenation. It leaves
  3: a command held in a `let`, a command taken as a parameter, and one whose only non-literal value is
  SQL in a quoted heredoc. The 3 literal-typed sites are silent: two `const` string literals and a
  `number`. 20 + 5 = 25.

The first draft reported 26: the site whose only non-literal value sat in a quoted heredoc was false,
since the shell expands nothing there. The heredoc scan removed it, and a site whose values sit unquoted
on the heredoc's opener line still reports.

## The findings, read one by one

**Real or latent bugs (6)**: a value from a person, an agent or a command line reaches the shell.

| Shape                                                         | Count | Reading                                                   |
| ------------------------------------------------------------- | ----- | --------------------------------------------------------- |
| Search words in a quoted SQL string inside the command        | 1     | **Bug**: an apostrophe breaks the search, `$(...)` runs   |
| A file path from a caller or the command line, inside `"..."` | 4     | Latent: a path with `"`, `$` or a backtick breaks or runs |
| Command-line arguments unquoted on a heredoc's opener line    | 1     | Latent: the arguments are split and expanded by `bash`    |

**True but harmless today (19)**: the shell does parse a non-literal value, but where it comes from
holds no shell syntax, or it is escaped correctly.

| Shape                                                                     | Count | Why harmless now                                                                                  |
| ------------------------------------------------------------------------- | ----- | ------------------------------------------------------------------------------------------------- |
| A script in `'...'` with every `'` escaped                                | 11    | Correct POSIX escaping; still a shell string                                                      |
| A database path found on disk, with integer keys, digits or fixed queries | 3     | Integer keys from SQLite output, `\D` stripped digits, or code-written queries with no `"` or `$` |
| A script path built from `randomUUID()`                                   | 1     | A UUID has no shell syntax                                                                        |
| `JSON.stringify` of a code-built path                                     | 2     | Code-built path; note `JSON.stringify` leaves `$` and backticks live                              |
| A value on a heredoc's opener line                                        | 1     | Every caller passes a constant; the SQL is in a quoted heredoc                                    |
| An authorization URL                                                      | 1     | The client id from a credentials file, the rest `encodeURIComponent`                              |

**False: 0.**

## Verification

- Fixtures both ways: the originating search before (fires on the command, through two `const`
  bindings) and after (`execFileSync('sqlite3', [file, sql])`, silent); 25 firing shapes, 26 silent
  shapes, the older `@types/node` module layout, and the no-checker decline.
- Mutation check, 25 mutants, each through `go test -overlay` and each killed: the module-name check,
  the module-block requirement, the `__promisify__` mapping, the shell gate, the shell option's type,
  the non-empty shell string, the built gate, the built gate through `const`, literal types as safe,
  the number flag, the type-parameter constraint, flattening through `const`, the single-literal
  splice, judging a conditional by branch, the literal-union heredoc decline, the conditional heredoc
  decline, the quoted-body skip, the heredoc closing line, `<<-` tab stripping, unquoted bodies staying
  judged, the unreadable-delimiter decline, the here-string skip, several openers in order, the
  argument list under a shell, and a delimiter holding a value.
