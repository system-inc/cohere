package unused_code

import (
	"crypto/sha256"
	"slices"
	"strings"
	"testing"
)

// differentialHostSources are a package whose Answer reads path through an embedded inner, so its body uses the
// embedded field and inner's path, both declared in types.go, past the body's own file.
var differentialHostSources = []recordedSource{
	{name: "shape.go", source: `package shape

func Answer() string {
	var answer compilerAgreementCase
	return answer.path
}
`},
	{name: "types.go", source: `package shape

type inner struct {
	path string
}

type compilerAgreementCase struct {
	inner
	group int
}
`},
}

// differentialBuildSources are the same package as a build compiles it: shape.go's bytes are the host's, and types.go
// keeps every place the host's record names but gives compilerAgreementCase a path of its own, after them, so the
// build's Answer reads that one and never inner's. The host's record still resolves end for end in this build, and
// spliced it would use the embedded field and inner's path, leaving the build's own path unused.
var differentialBuildSources = []recordedSource{
	differentialHostSources[0],
	{name: "types.go", source: `package shape

type inner struct {
	path string
}

type compilerAgreementCase struct {
	inner
	group int
	path  string
}
`},
}

// lyingHost is a host's side that answers body records as sides answers them, and the digest of any other file as
// the build's own bytes of it, build, hash, whatever the host read: a side whose digests cannot be trusted, so a
// record the build should not take passes every check of its files.
type lyingHost struct {
	sides recordsHost
	build []recordedSource
}

// BodyRecord is the record sides answers.
func (host lyingHost) BodyRecord(packageId string, filename string, offset int) (
	RecordedBody,
	[sha256.Size]byte,
	bool,
) {
	return host.sides.BodyRecord(packageId, filename, offset)
}

// FileDigest is the sha256 of the build's own bytes of filename.
func (host lyingHost) FileDigest(packageId string, filename string) ([sha256.Size]byte, bool) {
	if packageId != host.sides.identifier {
		return [sha256.Size]byte{}, false
	}
	for _, each := range host.build {
		if each.name == filename {
			return sha256.Sum256([]byte(each.source)), true
		}
	}
	return [sha256.Size]byte{}, false
}

// The record differential (#xeafx79, machine 2): a build in which Answer stands writes its own record of the body
// before it takes the host's, and walks the body where the two differ end for end, counted and named. Here the side
// lies about types.go, so the host's record passes every check of its files, and only the differential sees that
// the build's Answer reads another path: the run walks the body, names the use the records first disagree on, its
// read of path, inner's in the host's record and its own in the build's, and finds the plain verdict, the build's
// own path used and the embedded inner unused.
func TestARecordTheBuildWouldNotWriteIsWalked(test *testing.T) {
	test.Parallel()
	recorded := hostRecords(test, differentialHostSources)
	plainPass := recordedPass(test, differentialBuildSources, nil)
	plainVerdict, plainError := run(plainPass)
	if plainError != nil {
		test.Fatalf("running U1000 plainly: %v", plainError)
	}
	plainResult, isResult := plainVerdict.(result)
	if !isResult {
		test.Fatalf("U1000's result is %T", plainVerdict)
	}
	plain := wordedVerdict(plainPass.Fset, plainResult)
	if !slices.ContainsFunc(plain, func(worded string) bool {
		return strings.HasPrefix(worded, "unused field inner at types.go ")
	}) {
		test.Fatalf("the plain verdict is %v, which reports no embedded inner: the host's record would change nothing",
			plain)
	}

	host := lyingHost{sides: recordsHost{identifier: standingPackagePath, recorded: recorded},
		build: differentialBuildSources}
	verdict, spliced := splicedRun(test, differentialBuildSources, host)
	want := SplicedBodies{RecordsDiffer: 1}
	want.DifferingBodies[0] = DifferingBody{File: "shape.go", Offset: 15, Field: "uses[2]"}
	if spliced != want {
		test.Errorf("the run did %+v with Answer's body, want %+v", spliced, want)
	}
	if !slices.Equal(verdict, plain) {
		test.Errorf("the verdict is\n%v\nwant the plain run's\n%v", verdict, plain)
	}
}
