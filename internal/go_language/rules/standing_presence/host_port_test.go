package standing_presence

import (
	"go/types"
	"testing"
)

// hostport judges a package that imports fmt, and never net itself (#xeafx79, the reader's S3), whose own dial
// functions the original never finds: net, importing fmt, is not judged, and any other package importing fmt is.
func TestHostPortJudgesNoNetPackage(test *testing.T) {
	test.Parallel()

	formatting := types.NewPackage("fmt", "fmt")
	network := types.NewPackage("net", "net")
	network.SetImports([]*types.Package{formatting})
	dialing := types.NewPackage("example.com/dialing", "dialing")
	dialing.SetImports([]*types.Package{formatting})
	if judgesHostPort(network) {
		test.Errorf("hostport judges net, whose address variables the original never reads")
	}
	if !judgesHostPort(dialing) {
		test.Errorf("hostport does not judge a package importing fmt")
	}
}
