package unused_code

import "crypto/sha256"

// bodiesOnlyHost is a host's side that answers body records as sides answers them and is no FileDigestHost, as the
// engine's side is until it digests every file of its package.
type bodiesOnlyHost struct {
	sides recordsHost
}

// BodyRecord is the record sides answers.
func (host bodiesOnlyHost) BodyRecord(packageId string, filename string, offset int) (
	RecordedBody,
	[sha256.Size]byte,
	bool,
) {
	return host.sides.BodyRecord(packageId, filename, offset)
}

// RecordsHeld is what sides answers.
func (host bodiesOnlyHost) RecordsHeld(packageId string) bool {
	return host.sides.RecordsHeld(packageId)
}
