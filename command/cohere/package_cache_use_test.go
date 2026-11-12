package main

import (
	"encoding/json"
	"fmt"
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
		{cache: packageCacheUse{On: true, Hit: 4, Missed: 2, Written: 2, Reloads: 1, ReloadRounds: 1},
			want: "package cache hit 4, missed 2, wrote 2, 1 reloaded from source in 1 rounds"},
		{cache: packageCacheUse{FromBinary: 258, Reloads: 3, ReloadRounds: 2},
			want: "package cache off, standard library: 258 from the binary, 3 reloaded from source in 2 rounds"},
		{cache: packageCacheUse{Replays: 35}, want: "package cache off, 35 answers replayed"},
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

// The account and the summary `--json` writes name what kept the findings cache from being read or written, which
// the engine says one by one on stderr alone otherwise, and every write that failed, of which stderr says only the
// first (#27r8r24), and carry nothing of either when there was none, so a run without a problem encodes as it always
// has.
func TestThePackageCacheNamesItsProblems(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		cache packageCacheUse
		want  string
	}{
		{cache: packageCacheUse{On: true, Missed: 3, Problems: 2},
			want: "package cache hit 0, missed 3, wrote 0, 2 findings cache problems"},
		{cache: packageCacheUse{On: true, Missed: 412, Problems: 1, WriteFailed: 412},
			want: "package cache hit 0, missed 412, wrote 0, 1 findings cache problem, 412 writes failed"},
		{cache: packageCacheUse{On: true, Missed: 1, Problems: 1},
			want: "package cache hit 0, missed 1, wrote 0, 1 findings cache problem"},
		{cache: packageCacheUse{On: true, Hit: 4}, want: "package cache hit 4, missed 0, wrote 0"},
	} {
		if got := testCase.cache.line(); got != testCase.want {
			test.Errorf("got %q, want %q", got, testCase.want)
		}
		encoded := jsonLine(summaryAsJson(runSummary{PackageCache: &testCase.cache}))
		want := fmt.Sprintf(`"problems":%d`, testCase.cache.Problems)
		if carries := strings.Contains(encoded, want); carries != (testCase.cache.Problems > 0) {
			test.Errorf("want %s carried %t in %s", want, testCase.cache.Problems > 0, encoded)
		}
		if testCase.cache.Problems == 0 && strings.Contains(encoded, `"problems"`) {
			test.Errorf("want no count when it is 0, got %s", encoded)
		}
		failed := fmt.Sprintf(`"writeFailed":%d`, testCase.cache.WriteFailed)
		if carries := strings.Contains(encoded, failed); carries != (testCase.cache.WriteFailed > 0) {
			test.Errorf("want %s carried %t in %s", failed, testCase.cache.WriteFailed > 0, encoded)
		}
	}
}
