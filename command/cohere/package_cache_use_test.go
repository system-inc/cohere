package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The account's cache clause says the standard library's packages from the binary whether the store was on or not,
// so a `--no-cache` run that reused them reads as such, and says nothing of them when there were none. `--json`
// carries the count only when it is not 0, so a run without it encodes as it always has.
func TestThePackageCacheLineSaysTheStandardLibraryFromTheBinary(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name  string
		cache packageCacheUse
		want  string
	}{
		{name: "off", cache: packageCacheUse{}, want: "package cache off"},
		{
			name: "off, with the binary's entries", cache: packageCacheUse{FromBinary: 258},
			want: "package cache off, standard library: 258 from the binary",
		},
		{
			name: "on, with the binary's entries", cache: packageCacheUse{On: true, Hit: 4, Missed: 1, Written: 1,
				FromBinary: 258},
			want: "package cache hit 4, missed 1, wrote 1, standard library: 258 from the binary",
		},
		{name: "on", cache: packageCacheUse{On: true, Hit: 4}, want: "package cache hit 4, missed 0, wrote 0"},
	} {
		if got := testCase.cache.line(); got != testCase.want {
			test.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
	for _, testCase := range []struct {
		cache    packageCacheUse
		carrying bool
	}{{cache: packageCacheUse{FromBinary: 258}, carrying: true}, {cache: packageCacheUse{On: true}}} {
		encoded, encodeError := json.Marshal(testCase.cache)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		if carries := strings.Contains(string(encoded), `"standardLibraryFromBinary":258`); carries !=
			testCase.carrying {
			test.Errorf("want the count carried %t in %s", testCase.carrying, encoded)
		}
		if !testCase.carrying && strings.Contains(string(encoded), "standardLibraryFromBinary") {
			test.Errorf("want no count when it is 0, got %s", encoded)
		}
	}
}

// The account says the packages a run loaded again from source to answer a question of a fact computed on demand
// (#w7zmvat wave 3), whether the store was on or not, and nothing of them when there were none, and `--json` carries
// the count only when it is not 0.
func TestThePackageCacheLineSaysTheReloads(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		cache packageCacheUse
		want  string
	}{
		{cache: packageCacheUse{On: true, Hit: 4, Missed: 2, Written: 2, Reloads: 1},
			want: "package cache hit 4, missed 2, wrote 2, 1 reloaded from source"},
		{cache: packageCacheUse{FromBinary: 258, Reloads: 2},
			want: "package cache off, standard library: 258 from the binary, 2 reloaded from source"},
		{cache: packageCacheUse{On: true, Hit: 4}, want: "package cache hit 4, missed 0, wrote 0"},
	} {
		if got := testCase.cache.line(); got != testCase.want {
			test.Errorf("got %q, want %q", got, testCase.want)
		}
		encoded, encodeError := json.Marshal(testCase.cache)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		if carries := strings.Contains(string(encoded), `"reloads"`); carries != (testCase.cache.Reloads > 0) {
			test.Errorf("want the reloads carried %t in %s", testCase.cache.Reloads > 0, encoded)
		}
	}
}
