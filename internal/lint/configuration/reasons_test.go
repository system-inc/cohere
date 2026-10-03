package configuration

import (
	"path/filepath"
	"testing"
)

// Every off states its reason where coverage prints it (#2qq4yr7): a rule turned off with no sentence
// saying why is an allowance, the same failure a departure with no reason was. A file says why under
// `reasons`, and the list is held to the guard `departures` has, so a stale entry cannot pass as a
// decision.

func TestAReasonedOffLoadsAndIsRecorded(t *testing.T) {
	directory := writeConfigs(t, map[string]string{
		"CohereSettings.json": `{"rules": {"no-continue": "off"}, "reasons": {"no-continue": "  style, with no bug class behind it  "}}`,
	})
	loaded := loadOrFail(t, filepath.Join(directory, "CohereSettings.json"))
	offReason, recorded := loaded.OffReasons["no-continue"]
	if !recorded || offReason.Reason != "style, with no bug class behind it" {
		t.Fatalf("recorded %+v (%v), want the file's sentence, trimmed", offReason, recorded)
	}
	if offReason.File != filepath.Join(directory, "CohereSettings.json") {
		t.Errorf("the reason names %s, want the file that gave it", offReason.File)
	}
}

func TestAReasonThatExplainsNothingIsRefused(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "a reason for a rule the file turns on",
			files: map[string]string{"CohereSettings.json": `{"rules": {"no-continue": "error"}, "reasons": {"no-continue": "left over from when it was off"}}`},
			want:  `names "no-continue" under "reasons", but it does not turn that rule off`,
		},
		{
			name:  "a reason for a rule the file never writes",
			files: map[string]string{"CohereSettings.json": `{"reasons": {"no-continue": "stale"}}`},
			want:  `names "no-continue" under "reasons", but it does not turn that rule off`,
		},
		{
			// The off is the base's, so its reason belongs in the base.
			name: "a reason for an off another file wrote",
			files: map[string]string{
				"base.json":           `{"rules": {"no-continue": "off"}}`,
				"CohereSettings.json": `{"extends": "./base.json", "reasons": {"no-continue": "explained from the wrong file"}}`,
			},
			want: `names "no-continue" under "reasons", but it does not turn that rule off`,
		},
		{
			name:  "a reason with no sentence",
			files: map[string]string{"CohereSettings.json": `{"rules": {"no-continue": "off"}, "reasons": {"no-continue": "   "}}`},
			want:  `names "no-continue" under "reasons" and gives no reason`,
		},
		{
			name: "a reason beside the departure that already says why",
			files: map[string]string{
				"base.json": `{"rules": {"no-var": "error"}}`,
				"CohereSettings.json": `{"extends": "./base.json", "rules": {"no-var": "off"},
					"departures": {"no-var": "a legacy script keeps var"}, "reasons": {"no-var": "said twice"}}`,
			},
			want: `names "no-var" under both "reasons" and "departures"`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			directory := writeConfigs(t, testCase.files)
			refusedWith(t, filepath.Join(directory, "CohereSettings.json"), testCase.want)
		})
	}
}

// A reason follows its rule up the chain: a project inheriting a reasoned off inherits the reason, a
// restated off keeps it, an off that departs from an inherited ruling carries the departure's reason,
// and a project turning the rule back on drops it.
func TestAReasonFollowsTheRuleUpTheChain(t *testing.T) {
	base := `{"rules": {"no-continue": "off", "no-plusplus": "off", "no-var": "error"},
		"reasons": {"no-continue": "style", "no-plusplus": "style too"}}`
	directory := writeConfigs(t, map[string]string{
		"base.json": base,
		"CohereSettings.json": `{"extends": "./base.json",
			"rules": {"no-plusplus": "error", "no-continue": "off", "no-var": "off"},
			"departures": {"no-plusplus": "this project counts by hand", "no-var": "a legacy script keeps var"}}`,
	})
	loaded := loadOrFail(t, filepath.Join(directory, "CohereSettings.json"))

	if offReason := loaded.OffReasons["no-continue"]; offReason.Reason != "style" || offReason.File != filepath.Join(directory, "base.json") {
		t.Errorf("a restated off recorded %+v, want the base's reason and file", offReason)
	}
	if offReason := loaded.OffReasons["no-var"]; offReason.Reason != "a legacy script keeps var" {
		t.Errorf("an off departing from an inherited error recorded %+v, want the departure's reason", offReason)
	}
	if offReason, recorded := loaded.OffReasons["no-plusplus"]; recorded {
		t.Errorf("a rule the project turned back on kept a reason for being off: %+v", offReason)
	}
}

// Until every set and repository carries its reasons, an unreasoned off loads and is simply not
// recorded, which coverage reports as an allowance.
func TestAnUnreasonedOffIsNotRecorded(t *testing.T) {
	directory := writeConfigs(t, map[string]string{"CohereSettings.json": `{"rules": {"no-continue": "off"}}`})
	loaded := loadOrFail(t, filepath.Join(directory, "CohereSettings.json"))
	if offReason, recorded := loaded.OffReasons["no-continue"]; recorded {
		t.Errorf("an off no file explains was recorded with %+v", offReason)
	}
	if !loaded.TurnsOffAtTopLevel("no-continue") {
		t.Error("TurnsOffAtTopLevel did not see the off")
	}
}
