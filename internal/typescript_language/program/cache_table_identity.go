package program

import "fmt"

// CacheTableIdentity is what built the binary that writes or reads a table. It goes in the header, so a
// table written by any other build is discarded before a byte of its body is trusted.
type CacheTableIdentity struct {
	SelfCommit     string
	CompilerCommit string
	GoToolchain    string
	Platform       string
}

func (identity CacheTableIdentity) String() string {
	return fmt.Sprintf("cohere %s, compiler %s, %s, %s",
		orUnknown(identity.SelfCommit), orUnknown(identity.CompilerCommit), identity.GoToolchain, identity.Platform)
}
