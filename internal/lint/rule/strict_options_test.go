package rule

import (
	"encoding/json"
	"strings"
	"testing"
)

type strictInner struct {
	Pattern string `json:"pattern"`
}

type strictEmbedded struct {
	Shared bool `json:"shared"`
}

type strictCustom struct{ value string }

func (custom *strictCustom) UnmarshalJSON(raw []byte) error {
	custom.value = string(raw)
	return nil
}

type strictOptions struct {
	strictEmbedded
	EnforceForTsTypes bool          `json:"enforceForTSTypes"`
	Paths             []strictInner `json:"paths"`
	Ignore            []string      // untagged: reached through Go's case-insensitive match
	Custom            strictCustom  `json:"custom"`
}

// TestUnmarshalOptionsRefusesWhatUpstreamRefuses is #4n972g9: an unknown key, and a key spelled in
// a different case from its tag, are refused rather than dropped. The baseline case comes first, so
// a refusal below cannot be explained by the decoder refusing everything.
func TestUnmarshalOptionsRefusesWhatUpstreamRefuses(test *testing.T) {
	test.Parallel()
	var decoded strictOptions
	valid := `{"shared": true, "enforceForTSTypes": true, "paths": [{"pattern": "x"}], "ignore": ["a"], "custom": {"Anything": 1}}`
	if unmarshalOptionsError := UnmarshalOptions([]byte(valid), &decoded); unmarshalOptionsError != nil {
		test.Fatalf("baseline: a valid config was refused: %v", unmarshalOptionsError)
	}
	if !decoded.Shared || !decoded.EnforceForTsTypes || len(decoded.Paths) != 1 || decoded.Paths[0].Pattern != "x" ||
		len(decoded.Ignore) != 1 || decoded.Custom.value == "" {
		test.Fatalf("baseline: decoded %+v", decoded)
	}

	refused := []struct {
		name    string
		raw     string
		mention string
	}{
		{"unknown key at the top", `{"ignorePatern": ["a"]}`, "ignorePatern"},
		{"unknown key inside a list element", `{"paths": [{"pattern": "x", "message": "y"}]}`, "message"},
		{
			"tagged key in the wrong case",
			`{"enforceForTsTypes": true}`,
			`"enforceForTsTypes" is spelled "enforceForTSTypes"`,
		},
		{
			"tagged key in the wrong case inside a list",
			`{"paths": [{"Pattern": "x"}]}`,
			`"paths.0.Pattern" is spelled "paths.0.pattern"`,
		},
		{"promoted key in the wrong case", `{"Shared": true}`, `"Shared" is spelled "shared"`},
	}
	for _, testCase := range refused {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			var target strictOptions
			unmarshalOptionsError := UnmarshalOptions([]byte(testCase.raw), &target)
			if unmarshalOptionsError == nil || !strings.Contains(unmarshalOptionsError.Error(), testCase.mention) {
				test.Fatalf("want a refusal naming %q, got %v", testCase.mention, unmarshalOptionsError)
			}
		})
	}

	// The other direction, each a case plain json.Unmarshal also accepts: an untagged field reached
	// in any case, and a custom unmarshaler's contents, which are its own business.
	accepted := []string{`{"Ignore": ["a"]}`, `{"IGNORE": ["a"]}`, `{"custom": {"whatever": true}}`}
	for _, raw := range accepted {
		var target strictOptions
		if unmarshalOptionsError := UnmarshalOptions([]byte(raw), &target); unmarshalOptionsError != nil {
			test.Errorf("%s was refused: %v", raw, unmarshalOptionsError)
		}
		var plain strictOptions
		if decodeError := json.Unmarshal([]byte(raw), &plain); decodeError != nil {
			test.Errorf("control: plain json refused %s: %v", raw, decodeError)
		}
	}
}

// TestDecodeOptionsIntoIsStrict pins that the decoder 112 rules register through uses the strict
// path, so a refusal reaches the config layer with the type named.
func TestDecodeOptionsIntoIsStrict(test *testing.T) {
	test.Parallel()
	decode := DecodeOptionsInto[strictOptions]()
	if _, err := decode([]byte(`{"enforceForTSTypes": true}`)); err != nil {
		test.Fatalf("baseline: %v", err)
	}
	_, err := decode([]byte(`{"enforceForTsTypes": true}`))
	if err == nil || !strings.Contains(err.Error(), "rule.strictOptions") {
		test.Fatalf("want a refusal naming the options type, got %v", err)
	}
}
