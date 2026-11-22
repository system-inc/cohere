package unused_code

import "crypto/sha256"

// recordsHost is a host's side that answers what a recording run of U1000 handed on, as the engine's side answers
// it: for its package's identifier alone, by a body's key, with the digest of the file the body was parsed from, and
// nothing for a body whose file it holds no digest of.
type recordsHost struct {
	identifier string
	recorded   BodyRecords
}

// BodyRecord is the record keyed at filename and offset, and its parsed file's digest.
func (host recordsHost) BodyRecord(packageId string, filename string, offset int) (
	RecordedBody,
	[sha256.Size]byte,
	bool,
) {
	if packageId != host.identifier {
		return RecordedBody{}, [sha256.Size]byte{}, false
	}
	for _, body := range host.recorded.Bodies {
		if bodyFile, bodyOffset := body.Key(); bodyFile != filename || bodyOffset != offset {
			continue
		}
		digest, isDigested := host.recorded.Digests[body.ParsedFile]
		if !isDigested {
			return RecordedBody{}, [sha256.Size]byte{}, false
		}
		return body, digest, true
	}
	return RecordedBody{}, [sha256.Size]byte{}, false
}
