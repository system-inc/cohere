package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The `extends` chain (#rkm5a31): house rulings live once, in a base each project extends, so a ruling
// is made in one place and cannot drift between copies. Three copies of CohereSettings.json had drifted
// on fourteen rulings, one of them a security rule, before this existed.

// writeConfigs writes each named file under a fresh directory and returns that directory.
func writeConfigs(test *testing.T, files map[string]string) string {
	test.Helper()
	directory := test.TempDir()
	for name, contents := range files {
		path := filepath.Join(directory, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return directory
}

// loadOrFail loads a config that must load.
func loadOrFail(test *testing.T, path string) *Config {
	test.Helper()
	loaded, loadError := Load(path)
	if loadError != nil {
		test.Fatalf("loading %s: %v", path, loadError)
	}
	return loaded
}

// refusedWith loads a config that must be refused, and checks the error says why.
func refusedWith(test *testing.T, path string, want string) {
	test.Helper()
	_, loadError := Load(path)
	if loadError == nil {
		test.Fatalf("loading %s succeeded; it must be refused (%s)", path, want)
	}
	if !strings.Contains(loadError.Error(), want) {
		test.Fatalf(
			"loading %s was refused, but not for the reason under test.\n  got:  %v\n  want: %s",
			path,
			loadError,
			want,
		)
	}
}

func TestExtendsInheritsEveryRuleTheProjectDoesNotWrite(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"nexus/CohereSettings.json": `{"rules": {"no-var": "error", "eqeqeq": ["error", "always"]}}`,
		"CohereSettings.json":       `{"extends": "./nexus/CohereSettings.json", "rules": {"project-only": "error"}}`,
	})
	loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))

	for _, name := range []string{"no-var", "eqeqeq", "project-only"} {
		if loaded.Rules[name].Severity != SeverityError {
			test.Errorf("%s is %v after the merge, want error", name, loaded.Rules[name].Severity)
		}
	}
	if got := string(loaded.Rules["eqeqeq"].Options[0]); got != `"always"` {
		test.Errorf("eqeqeq's inherited option is %s, want \"always\"", got)
	}
}

// A bare severity keeps the inherited options, as ESLint does. Both halves, because a merge that
// always replaced would pass the first and one that always kept would pass the second.
func TestABareSeverityKeepsTheInheritedOptionsAndATupleReplacesThem(test *testing.T) {
	test.Parallel()
	base := `{"rules": {"max-classes-per-file": ["error", {"ignoreExpressions": true}]}}`

	test.Run("bare severity", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json": base,
			"CohereSettings.json": `{"extends": "./base.json",
				"departures": {"max-classes-per-file": "warn while the backlog clears"},
				"rules": {"max-classes-per-file": "warn"}}`,
		})
		setting := loadOrFail(test, filepath.Join(directory, "CohereSettings.json")).Rules["max-classes-per-file"]
		if setting.Severity != SeverityWarn {
			test.Errorf("severity is %v, want warn", setting.Severity)
		}
		if len(setting.Options) != 1 || compactJson(setting.Options[0]) != `{"ignoreExpressions":true}` {
			test.Errorf("options are %q, want the inherited {\"ignoreExpressions\":true}", setting.Options)
		}
	})

	test.Run("tuple", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json": base,
			"CohereSettings.json": `{"extends": "./base.json",
				"departures": {"max-classes-per-file": "this project counts expressions"},
				"rules": {"max-classes-per-file": ["error", {"ignoreExpressions": false}]}}`,
		})
		setting := loadOrFail(test, filepath.Join(directory, "CohereSettings.json")).Rules["max-classes-per-file"]
		if len(setting.Options) != 1 || compactJson(setting.Options[0]) != `{"ignoreExpressions":false}` {
			test.Errorf("options are %q, want the project's {\"ignoreExpressions\":false}", setting.Options)
		}
	})
}

// The guard this whole change exists for. Each case is the drift #rkm5a31 measured, written as the
// overlay that would have hidden it.
// Not parallel: it swaps the package's setFiles for a stand-in tier set through withOurTierForTest
// (withSetFiles), which every test that reads a set would see.
func TestADepartureFromAnInheritedRulingMustSayWhy(test *testing.T) {
	withOurTierForTest(test)
	base := `{"extends": "cohere:system-inc/test", "rules": {"no-implied-eval": "error", "guard-for-in": "off", "eqeqeq": ["error", {"null": "ignore"}]},
		"reasons": {"guard-for-in": "nexus/consistency-no-for-in replaces it"}}`

	// The positive case first, so every refusal below is shown to come from the guard under test
	// rather than from a fixture that never loaded.
	test.Run("a declared departure loads and is recorded", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json": base,
			"CohereSettings.json": `{"extends": "./base.json",
				"departures": {"guard-for-in": "this project has no for-in replacement yet"},
				"rules": {"guard-for-in": "error"}}`,
		})
		loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
		departure, recorded := loaded.Departures["guard-for-in"]
		if !recorded || departure.Reason != "this project has no for-in replacement yet" {
			test.Fatalf("departure recorded as %+v (%v), want the reason the file gave", departure, recorded)
		}
		if departure.File != filepath.Join(directory, "CohereSettings.json") {
			test.Errorf("departure names %s, want the project file", departure.File)
		}
	})

	test.Run("an undeclared departure is refused", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json":           base,
			"CohereSettings.json": `{"extends": "./base.json", "rules": {"no-implied-eval": "off"}}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), `sets "no-implied-eval" differently`)
	})

	test.Run("a reason of only whitespace is no reason", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json": base,
			"CohereSettings.json": `{"extends": "./base.json",
				"departures": {"no-implied-eval": "   "}, "rules": {"no-implied-eval": "off"}}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), `sets "no-implied-eval" differently`)
	})

	test.Run("a changed option is a departure", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json":           base,
			"CohereSettings.json": `{"extends": "./base.json", "rules": {"eqeqeq": ["error", {"null": "always"}]}}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), `sets "eqeqeq" differently`)
	})

	test.Run("restating the ruling is not a departure, whatever the whitespace", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json":           base,
			"CohereSettings.json": `{"extends": "./base.json", "rules": {"eqeqeq": ["error", { "null" :  "ignore" }]}}`,
		})
		loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
		if len(loaded.Departures) != 0 {
			test.Errorf("a restatement was recorded as a departure: %+v", loaded.Departures)
		}
	})

	test.Run("a departure entry that departs from nothing is refused", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json": base,
			"CohereSettings.json": `{"extends": "./base.json",
				"departures": {"no-implied-eval": "left over from before the base said error"},
				"rules": {"no-implied-eval": "error"}}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), `names "no-implied-eval" under "departures"`)
	})

	test.Run("a departure entry for a rule the file never writes is refused", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json":           base,
			"CohereSettings.json": `{"extends": "./base.json", "departures": {"guard-for-in": "stale"}}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), `names "guard-for-in" under "departures"`)
	})
}

// A project cannot step around a house ruling by spelling the key differently. Both directions,
// because the resolver's own matching is one-way and this check must not be.
// Not parallel: it swaps the package's setFiles for a stand-in tier set through withOurTierForTest
// (withSetFiles), which every test that reads a set would see.
func TestADepartureCannotHideBehindAnotherSpelling(test *testing.T) {
	// Only the bare name is registered, so the two spellings reach one rule: a respelling.
	withOurTierForTest(test)
	registered := []string{"consistency-no-enum"}
	cases := []struct {
		name    string
		base    string
		project string
		written string
		dropped string
	}{
		{
			"bare over qualified",
			"nexus/consistency-no-enum",
			"consistency-no-enum",
			"consistency-no-enum",
			"nexus/consistency-no-enum",
		},
		{
			"qualified over bare",
			"consistency-no-enum",
			"nexus/consistency-no-enum",
			"nexus/consistency-no-enum",
			"consistency-no-enum",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			undeclared := writeConfigs(test, map[string]string{
				"base.json": `{"extends": "cohere:system-inc/test", "rules": {"` +
					testCase.base +
					`": "error"}}`,
				"CohereSettings.json": `{"extends": "./base.json", "rules": {"` + testCase.project + `": "off"}}`,
			})
			if _, loadForError := LoadFor(
				filepath.Join(undeclared, "CohereSettings.json"),
				registered,
			); loadForError == nil ||
				!strings.Contains(loadForError.Error(), "differently") {
				test.Fatalf("a respelled departure with no reason loaded: %v", loadForError)
			}

			declared := writeConfigs(test, map[string]string{
				"base.json": `{"rules": {"` + testCase.base + `": "error"}}`,
				"CohereSettings.json": `{"extends": "./base.json",
					"departures": {"` + testCase.project + `": "enums are generated here"},
					"rules": {"` + testCase.project + `": "off"}}`,
			})
			loaded, loadForError := LoadFor(filepath.Join(declared, "CohereSettings.json"), registered)
			if loadForError != nil {
				test.Fatal(loadForError)
			}
			// One key per ruling, or the resolver holds two spellings and calls the rule ambiguous.
			if _, kept := loaded.Rules[testCase.dropped]; kept {
				test.Errorf(
					"the inherited spelling %q is still in the merged rules beside %q",
					testCase.dropped,
					testCase.written,
				)
			}
			if loaded.Resolve("source/File.ts").Enabled("consistency-no-enum") {
				test.Error("the project's off did not reach the rule")
			}
		})
	}
}

// Two spellings of one rule in the same file are that file's own business, as they were before
// `extends` existed, and must never read as one inheriting from the other. Repeated, because Go's map
// order decides which spelling the loop reaches first.
//
// The pair must be one spelling qualifying the other (`no-shadow` and `nexus/no-shadow`). Two
// siblings such as `@typescript-eslint/no-shadow` and `nexus/no-shadow` never match each other, so a
// fixture built from them passes whether or not the layer compares against its own rules: this test
// was first written that way and a mutant comparing against the live map survived it.
func TestTwoSpellingsInOneFileAreNeverADeparture(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"base.json": `{"rules": {"eqeqeq": "error"}}`,
		"CohereSettings.json": `{"extends": "./base.json",
			"rules": {"no-shadow": "error", "nexus/no-shadow": "off"}, "reasons": {"nexus/no-shadow": "the twin spelling is off"}}`,
	})
	for attempt := 0; attempt < 50; attempt++ {
		loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
		if len(loaded.Departures) != 0 || len(loaded.Rules) < 3 {
			test.Fatalf("attempt %d: departures %+v, rules %v", attempt, loaded.Departures, loaded.Rules)
		}
	}
}

func TestPluginsUnionAndTheirDefaultsAreComputedOverTheMergedRules(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"structure.json": `{"plugins": ["react"], "rules": {"react/no-children-prop": "off"}, "reasons": {"react/no-children-prop": "turned off on purpose"}}`,
		"CohereSettings.json": `{"extends": "./structure.json", "plugins": ["react", "@typescript-eslint"],
			"rules": {}}`,
	})
	loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))

	if strings.Join(loaded.Plugins, ",") != "react,@typescript-eslint" {
		test.Errorf("plugins are %v, want the union in first-seen order", loaded.Plugins)
	}
	// The base's explicit off is a decision, and a plugin default must not overwrite it.
	if loaded.Rules["react/no-children-prop"].Severity != SeverityOff {
		test.Error("a plugin default overwrote the base's explicit off")
	}
	// A default no layer names still arrives, from a plugin only the base declared.
	if loaded.Rules["react/no-find-dom-node"].Severity != PluginDefaultSeverity {
		test.Error("a plugin declared only in the base contributed no defaults")
	}
}

// The base's blocks come first so the project's still win, and every pattern resolves against the
// project root, not the base's directory.
func TestOverridesAndIgnorePatternsConcatenateBaseFirst(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"libraries/nexus/CohereSettings.json": `{
			"rules": {"no-console": "error"},
			"ignorePatterns": ["**/generated/**"],
			"overrides": [{"files": ["**/*.test.ts"], "rules": {"no-console": "off"}}]}`,
		"CohereSettings.json": `{"extends": "./libraries/nexus/CohereSettings.json",
			"ignorePatterns": ["data/**"],
			"overrides": [{"files": ["scripts/**/*.test.ts"], "rules": {"no-console": "error"}}]}`,
	})
	loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))

	if strings.Join(loaded.IgnorePatterns, ",") != "**/generated/**,data/**" {
		test.Errorf("ignorePatterns are %v, want the base's first", loaded.IgnorePatterns)
	}
	// The format walk reads the same list without decoding rules, and must read it identically.
	if shared, ignorePatternsOfError := IgnorePatternsOf(
		filepath.Join(directory, "CohereSettings.json"),
	); ignorePatternsOfError != nil || strings.Join(shared, ",") != strings.Join(loaded.IgnorePatterns, ",") {
		test.Errorf("IgnorePatternsOf = %v (%v), want Load's %v", shared, ignorePatternsOfError, loaded.IgnorePatterns)
	}
	if loaded.Root != directory {
		test.Errorf("root is %s, want the project directory %s", loaded.Root, directory)
	}
	if loaded.Resolve(filepath.Join(directory, "source/Thing.test.ts")).Enabled("no-console") {
		test.Error("the base's override did not apply to a test file under the project root")
	}
	if !loaded.Resolve(filepath.Join(directory, "scripts/Run.test.ts")).Enabled("no-console") {
		test.Error("the project's later override did not win over the base's")
	}
	if !loaded.Resolve(filepath.Join(directory, "source/Thing.ts")).Enabled("no-console") {
		test.Error("the base's override leaked onto a file it does not match")
	}
}

func TestAChainOfThreeMergesInOrderAndListsEverySource(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"structure/nexus/CohereSettings.json": `{"rules": {"no-var": "error", "guard-for-in": "off"},
			"reasons": {"guard-for-in": "nexus/consistency-no-for-in replaces it"}}`,
		"structure/CohereSettings.json": `{"extends": "./nexus/CohereSettings.json",
			"rules": {"react/no-danger": "error"}}`,
		"CohereSettings.json": `{"extends": "./structure/CohereSettings.json",
			"departures": {"no-var": "a legacy script keeps var on purpose"}, "rules": {"no-var": "off"}}`,
	})
	loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))

	want := []string{
		filepath.Join(directory, "CohereSettings.json"),
		filepath.Join(directory, "structure/CohereSettings.json"),
		filepath.Join(directory, "structure/nexus/CohereSettings.json"),
	}
	if strings.Join(loaded.Sources, "\n") != strings.Join(want, "\n") {
		test.Errorf("sources are\n  %s\nwant\n  %s", strings.Join(loaded.Sources, "\n  "), strings.Join(want, "\n  "))
	}
	if loaded.Rules["no-var"].Severity != SeverityOff || loaded.Rules["react/no-danger"].Severity != SeverityError ||
		loaded.Rules["guard-for-in"].Severity != SeverityOff {
		test.Errorf("merged rules are %v", loaded.Rules)
	}
}

func TestAFileWithoutExtendsListsOnlyItself(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{"CohereSettings.json": `{"rules": {"no-var": "error"}}`})
	loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
	if len(loaded.Sources) != 1 || loaded.Sources[0] != filepath.Join(directory, "CohereSettings.json") {
		test.Errorf("sources are %v", loaded.Sources)
	}
}

func TestABrokenChainRefusesTheWholeLoad(test *testing.T) {
	test.Parallel()
	test.Run("a missing base", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"CohereSettings.json": `{"extends": "./nexus/CohereSettings.json", "rules": {}}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), "reading lint config")
	})

	test.Run("a cycle", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"a.json":              `{"extends": "./b.json"}`,
			"b.json":              `{"extends": "./a.json"}`,
			"CohereSettings.json": `{"extends": "./a.json"}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), "extends itself")
	})

	test.Run("a base with a key the loader does not implement", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json":           `{"rulez": {}}`,
			"CohereSettings.json": `{"extends": "./base.json"}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), "does not implement")
	})
}

// `settings` are read from the project's own file only, as ESLint reads them from the project's
// config, so a value in a base would be ignored silently. `format` has a reader that follows the chain
// (formatoptions applies each file's block over the one it extends), so a base may carry it. Both still
// load from the project's own file.
func TestABaseMayCarryOnlyKeysWhoseReadersFollowTheChain(test *testing.T) {
	test.Parallel()
	for key, followed := range map[string]bool{"settings": false, "format": true} {
		test.Run(key, func(test *testing.T) {
			test.Parallel()
			inBase := writeConfigs(test, map[string]string{
				"base.json":           `{"` + key + `": {}, "rules": {}}`,
				"CohereSettings.json": `{"extends": "./base.json"}`,
			})
			if followed {
				loadOrFail(test, filepath.Join(inBase, "CohereSettings.json"))
			} else {
				refusedWith(test, filepath.Join(inBase, "CohereSettings.json"), `declares "`+key+`"`)
			}

			inProject := writeConfigs(test, map[string]string{
				"base.json":           `{"rules": {}}`,
				"CohereSettings.json": `{"extends": "./base.json", "` + key + `": {}}`,
			})
			loadOrFail(test, filepath.Join(inProject, "CohereSettings.json"))
		})
	}
}

// An override that matches every file of its kind is a top-level rule written in another place, so
// changing an inherited ruling there needs a stated reason too (#25benkk). api's
// `["**/*.ts", "**/*.tsx"]` block turned off thirteen rulings the Nexus tier holds at error, and with
// only top-level entries checked it would have loaded with no reason and printed nothing.
// Not parallel: it swaps the package's setFiles for a stand-in tier set through withOurTierForTest
// (withSetFiles), which every test that reads a set would see.
func TestAWholeTreeOverrideDepartsLikeATopLevelRule(test *testing.T) {
	withOurTierForTest(test)
	base := `{"extends": "cohere:system-inc/test", "rules": {"no-const-assign": "error", "no-var": "error"}}`
	wholeTree := `"overrides": [{"files": ["**/*.ts", "**/*.tsx"], "rules": {"no-const-assign": "off", "no-var": "error"}}]`

	test.Run("without a reason it is refused", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json":           base,
			"CohereSettings.json": `{"extends": "./base.json", ` + wholeTree + `}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), `overrides "no-const-assign" for every file`)
	})

	test.Run("with a reason it loads and the departure prints", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json": base,
			"CohereSettings.json": `{"extends": "./base.json",
				"departures": {"no-const-assign": "TypeScript's checker reports it here"}, ` + wholeTree + `}`,
		})
		loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
		if loaded.Departures["no-const-assign"].Reason != "TypeScript's checker reports it here" {
			test.Errorf("the override's departure was not recorded: %+v", loaded.Departures)
		}
		// The restated no-var is not a departure, so it must not be recorded as one.
		if _, recorded := loaded.Departures["no-var"]; recorded {
			test.Error("a whole-tree override restating the inherited ruling was recorded as a departure")
		}
	})

	test.Run("a reason for a restated rule is stale and refused", func(test *testing.T) {
		test.Parallel()
		directory := writeConfigs(test, map[string]string{
			"base.json": base,
			"CohereSettings.json": `{"extends": "./base.json",
				"departures": {"no-const-assign": "checked by TypeScript", "no-var": "left over"}, ` + wholeTree + `}`,
		})
		refusedWith(test, filepath.Join(directory, "CohereSettings.json"), `names "no-var" under "departures"`)
	})

	// The other side, and the one a too-broad check would break: scoped overrides are a project's own
	// business and need no reason, or every generated-code or test-file block would have to explain itself.
	for _, files := range []string{
		`["**/generated/**/*.{ts,tsx}"]`,
		`["**/*.test.ts"]`,
		`["source/**/*.ts"]`,
		`["modules/**"]`,
	} {
		test.Run("a scoped override needs no reason: "+files, func(test *testing.T) {
			test.Parallel()
			directory := writeConfigs(test, map[string]string{
				"base.json": base,
				"CohereSettings.json": `{"extends": "./base.json",
					"overrides": [{"files": ` + files + `, "rules": {"no-const-assign": "off"}}]}`,
			})
			if loaded := loadOrFail(
				test,
				filepath.Join(directory, "CohereSettings.json"),
			); len(loaded.Departures) != 0 {
				test.Errorf("a scoped override was recorded as a departure: %+v", loaded.Departures)
			}
		})
	}
}

// The shape test decides whole-tree from the pattern alone. Both directions, because a test of only
// the matches passes a predicate that answers yes to everything.
func TestWholeTreePatternsAreDecidedByShape(test *testing.T) {
	test.Parallel()
	cases := []struct {
		patterns []string
		want     bool
	}{
		{[]string{"**/*.ts", "**/*.tsx"}, true},
		{[]string{"**/*.{ts,tsx}"}, true},
		{[]string{"**/*"}, true},
		{[]string{"**/*.ts", "source/**/*.ts"}, false},
		{[]string{"**/*.test.ts"}, false},
		{[]string{"**/generated/**/*.ts"}, false},
		{[]string{"*.ts"}, false},
		{[]string{"modules/**"}, false},
		{nil, false},
	}
	for _, testCase := range cases {
		if got := coversEveryFileOfItsKind(testCase.patterns); got != testCase.want {
			test.Errorf("coversEveryFileOfItsKind(%v) = %v, want %v", testCase.patterns, got, testCase.want)
		}
	}
}

// A core rule and its typescript-eslint twin are two registered rules, and a project writing one must
// not unconfigure the other (#hprjh4s). The Nexus tier holds the twin at error; api wrote the core key
// off. The two still share a ruling, because the resolver lets the qualified key configure the core
// rule when no key names it exactly, so a reason is required; but the twin keeps its own key.
// Not parallel: it swaps the package's setFiles for a stand-in tier set through withOurTierForTest
// (withSetFiles), which every test that reads a set would see.
func TestATwinRuleKeepsItsOwnKeyWhenAProjectWritesTheOther(test *testing.T) {
	registered := []string{"no-invalid-this", "@typescript-eslint/no-invalid-this"}
	directory := writeConfigs(test, map[string]string{
		"base.json": `{"rules": {"@typescript-eslint/no-invalid-this": "error"}}`,
		"CohereSettings.json": `{"extends": "./base.json",
			"departures": {"no-invalid-this": "the core rule is off here"},
			"rules": {"no-invalid-this": "off"}}`,
	})
	loaded, loadForError := LoadFor(filepath.Join(directory, "CohereSettings.json"), registered)
	if loadForError != nil {
		test.Fatal(loadForError)
	}
	resolved := loaded.Resolve("source/File.ts")
	if !resolved.Enabled("@typescript-eslint/no-invalid-this") {
		test.Error("the twin the project never wrote was unconfigured by the merge")
	}
	if resolved.Enabled("no-invalid-this") {
		test.Error("the project's off did not reach the core rule")
	}

	// The control, so the departure demand is shown to come from the shared ruling: with no reason the
	// same project is refused.
	withOurTierForTest(test)
	undeclared := writeConfigs(test, map[string]string{
		"base.json":           `{"extends": "cohere:system-inc/test", "rules": {"@typescript-eslint/no-invalid-this": "error"}}`,
		"CohereSettings.json": `{"extends": "./base.json", "rules": {"no-invalid-this": "off"}}`,
	})
	if _, loadForError := LoadFor(filepath.Join(undeclared, "CohereSettings.json"), registered); loadForError == nil {
		test.Error("the core key changed the ruling the twin's key carried for it, and no reason was demanded")
	}
}

// Without registered names only the same key is the same ruling, so two spellings are two entries and
// neither is removed.
func TestWithoutTheRegistryOnlyTheSameKeyIsTheSameRuling(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"base.json":           `{"rules": {"nexus/consistency-no-enum": "error"}}`,
		"CohereSettings.json": `{"extends": "./base.json", "rules": {"consistency-no-enum": "off"}, "reasons": {"consistency-no-enum": "a second spelling, off"}}`,
	})
	loaded := loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
	if _, kept := loaded.Rules["nexus/consistency-no-enum"]; !kept {
		test.Error("a key was removed on the strength of its spelling alone")
	}
}

// An override block takes "files", "rules" and "reason", and nothing else loads. ESLint's
// `excludedFiles` is the case that matters: dropped silently, it widens the block to the files its
// author meant to leave out. The control is the same block with a reason, which loads and carries the
// reason and the file that wrote it, so the refusal is about the unknown key and nothing else.
func TestAnOverrideKeyTheLoaderDoesNotReadIsRefused(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{
		"excluded.json": `{"overrides": [{"files": ["**/*.ts"], "excludedFiles": ["**/keep.ts"], "rules": {"no-var": "off"}}]}`,
		"reasoned.json": `{"overrides": [{"files": ["**/generated/*.ts"], "reason": "  until the generator is fixed ", "rules": {"no-var": "off"}}]}`,
	})
	refusedWith(test, filepath.Join(directory, "excluded.json"), `override 0 declares "excludedFiles"`)

	reasoned := filepath.Join(directory, "reasoned.json")
	loaded := loadOrFail(test, reasoned)
	if len(loaded.Overrides) != 1 {
		test.Fatalf("the reasoned override did not load as one block: %+v", loaded.Overrides)
	}
	if loaded.Overrides[0].Reason != "until the generator is fixed" {
		test.Errorf("the reason was not carried, trimmed: %q", loaded.Overrides[0].Reason)
	}
	want, absoluteError := filepath.Abs(reasoned)
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	if loaded.Overrides[0].File != want {
		test.Errorf("the override names %q as its file, want %q", loaded.Overrides[0].File, want)
	}
}

// The project's own `settings` reach the run as written, keyed by plugin namespace, and a config
// without them carries none. Which namespaces and keys are read is the rule packages' to check
// (registry.CheckSettings); the loader only hands them through.
func TestTheProjectsOwnSettingsAreHandedThrough(test *testing.T) {
	test.Parallel()
	written := writeConfigs(test, map[string]string{
		"base.json":           `{"rules": {}}`,
		"CohereSettings.json": `{"extends": "./base.json", "settings": {"better-tailwindcss": {"callees": ["cn"]}}}`,
	})
	loaded := loadOrFail(test, filepath.Join(written, "CohereSettings.json"))
	if got := compactJson(loaded.Settings["better-tailwindcss"]); got != `{"callees":["cn"]}` {
		test.Fatalf("settings[\"better-tailwindcss\"] = %s, want the project's own block as written", got)
	}

	without := writeConfigs(test, map[string]string{"CohereSettings.json": `{"rules": {}}`})
	if settings := loadOrFail(test, filepath.Join(without, "CohereSettings.json")).Settings; settings != nil {
		test.Fatalf("a config with no settings carries %v", settings)
	}
}
