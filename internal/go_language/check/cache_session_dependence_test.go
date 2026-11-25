package check

import (
	"crypto/sha256"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/cache"
)

// Each kind of dependence record is kept under a key its version's text is hashed into, the one place a version is
// read (#mtfdb1m), so a record the previous version wrote is under another key and never read: the host's records at
// dependence-record-9 and the standard library's at standard-library-dependence-record-9, each named here as its text
// is, so a version moved back fails the first check, and the second with it. Each key is derived as the store's reader
// derives it (DependenceRecordKey, StandardLibraryDependenceRecordKey) and must be the one the named text is hashed
// into, so a version left out of its key fails the second alone. Those two are the whole pin: a record of 6 was kept
// under the key 6's text is hashed into, which is not 9's, so a third check against it could fail only where the
// second does.
func TestEachDependenceRecordIsReadOnlyUnderItsOwnVersionsKey(test *testing.T) {
	test.Parallel()

	key := cache.DigestOf([]byte("a package's first key"))
	hostKey := func(version string) cache.Key {
		return sha256.Sum256(append([]byte("dependence record "+version+" "), key[:]...))
	}
	standardLibraryKey := func(version string) cache.Key {
		return cache.DigestOf([]byte("standard library dependence record "+version), []byte("executable"),
			[]byte("toolchain"), key[:])
	}
	for _, each := range []struct {
		name    string
		version string
		want    string
		derived cache.Key
		at      func(string) cache.Key
	}{
		{"the host's", DependenceRecordVersion, "dependence-record-9", DependenceRecordKey(key), hostKey},
		{"the standard library's", StandardLibraryDependenceRecordVersion, "standard-library-dependence-record-9",
			StandardLibraryDependenceRecordKey(key, "executable", "toolchain"), standardLibraryKey},
	} {
		if each.version != each.want {
			test.Errorf("%s records are at %q, want %q", each.name, each.version, each.want)
		}
		if each.derived != each.at(each.want) {
			test.Errorf("%s record key is not the one %q is hashed into", each.name, each.want)
		}
	}
}
