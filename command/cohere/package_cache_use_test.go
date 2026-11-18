package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The account's cache clause says the standard library's packages from the system cache whether the store was on or
// not, so a `--no-cache` run that reused them reads as such, and says nothing of them when there were none. `--json`
// carries the count only when it is not 0, so a run without it encodes as it always has.
func TestThePackageCacheLineSaysTheStandardLibraryFromTheSystemCache(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name  string
		cache packageCacheUse
		want  string
	}{
		{name: "off", cache: packageCacheUse{}, want: "package cache off"},
		{
			name: "off, with the system cache's entries", cache: packageCacheUse{FromSystemCache: 258},
			want: "package cache off, standard library: 258 from the system cache",
		},
		{
			name: "on, with the system cache's entries", cache: packageCacheUse{On: true, Hit: 4, Missed: 1, Written: 1,
				FromSystemCache: 258},
			want: "package cache hit 4, missed 1, wrote 1, standard library: 258 from the system cache",
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
	}{{cache: packageCacheUse{FromSystemCache: 258}, carrying: true}, {cache: packageCacheUse{On: true}}} {
		encoded, encodeError := json.Marshal(testCase.cache)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		if carries := strings.Contains(string(encoded), `"standardLibraryFromSystemCache":258`); carries !=
			testCase.carrying {
			test.Errorf("want the count carried %t in %s", testCase.carrying, encoded)
		}
		if !testCase.carrying && strings.Contains(string(encoded), "standardLibraryFromSystemCache") {
			test.Errorf("want no count when it is 0, got %s", encoded)
		}
	}
}

// The account's cache clause says what the system cache did (#syscache): the entries the run wrote to it beside the
// packages it restored, and, under `--no-system-cache`, that it was off, so a run that computed the standard library
// says why. `--json` carries each only when it is set.
func TestThePackageCacheLineSaysWhatTheSystemCacheDid(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		cache packageCacheUse
		want  string
	}{
		{
			cache: packageCacheUse{On: true, Hit: 4, FromSystemCache: 258, SystemCacheWritten: 12},
			want: "package cache hit 4, missed 0, wrote 0, standard library: 258 from the system cache, 12 entries " +
				"written to the system cache",
		},
		{cache: packageCacheUse{SystemCacheOff: true}, want: "package cache off, system cache off (--no-system-cache)"},
		{
			cache: packageCacheUse{On: true, Hit: 4, SystemCacheOff: true},
			want:  "package cache hit 4, missed 0, wrote 0, system cache off (--no-system-cache)",
		},
	} {
		if got := testCase.cache.line(); got != testCase.want {
			test.Errorf("got %q, want %q", got, testCase.want)
		}
	}
	encoded, encodeError := json.Marshal(packageCacheUse{On: true})
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if strings.Contains(string(encoded), "systemCache") {
		test.Errorf("want no system cache field when nothing is set, got %s", encoded)
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
		{cache: packageCacheUse{FromSystemCache: 258, Reloads: 3, ReloadRounds: 2},
			want: "package cache off, standard library: 258 from the system cache, 3 reloaded from source in 2 rounds"},
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

// The account says what the extra builds did with their packs (#b2t9xmf): a warm run whose packs served every record
// says only the packs read and the records served, and a run that found a pack missing, read records whose shard had
// moved or that no pack held, refused a pack or wrote one says each, the refusals by why. `--json` carries the packs
// whole, and a run with no extra build carries none.
func TestThePackageCacheLineSaysWhatTheBuildsPacksDid(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		packs *buildPacksUse
		want  string
	}{
		{packs: &buildPacksUse{Read: 14, RecordsServed: 10920},
			want: "package cache hit 4, missed 0, wrote 0, builds' packs: 14 read, 10,920 records served"},
		{packs: &buildPacksUse{Read: 12, Missing: 2, RecordsServed: 1, RecordsStale: 5, RecordsMissed: 3,
			RefusedBy: map[string]int{"sum": 1, "version": 1}, Written: 4},
			want: "package cache hit 4, missed 0, wrote 0, builds' packs: 12 read, 1 record served, 2 missing, " +
				"5 stale, 3 not held, 2 refused (sum 1, version 1), 4 written"},
		{want: "package cache hit 4, missed 0, wrote 0"},
	} {
		cache := packageCacheUse{On: true, Hit: 4, Packs: testCase.packs}
		if got := cache.line(); got != testCase.want {
			test.Errorf("got %q, want %q", got, testCase.want)
		}
		encoded := jsonLine(summaryAsJson(runSummary{PackageCache: &cache}))
		if carries := strings.Contains(encoded, `"packs":{"read":`); carries != (testCase.packs != nil) {
			test.Errorf("want the packs carried %t in %s", testCase.packs != nil, encoded)
		}
	}
}
