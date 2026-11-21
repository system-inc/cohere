package unused_code

import (
	"testing"
)

// goldenSources is a package whose one body fills every part of a record: a local, the local's owner, a use of an
// object declared outside the body, a use from that object's field to the local's through a struct conversion, a
// local the root uses through a needed ignore directive, and a file the body reads.
var goldenSources = []recordedSource{{"shape.go", `package shape

type outer struct{ field int }

func Use() int {
	//lint:ignore U1000 kept for the golden
	const kept = 1
	type inner struct{ field int }
	return outer(inner{}).field
}
`}}

// goldenRecord is the exact canonical form of Use's record. It pins what encoded writes byte for byte, so a change to
// the form, a field reordered, a tag renamed, a list left nil, is red here and not only where two runs of the same
// code are compared with each other.
const goldenRecord = `` +
	`{"body":{"file":"shape.go","offset":47},"locals":[{"place":{"file":"shape.go","offset":127},` +
	`"kind":"type","name":"inner","owners":[{"kind":"function","local":0,"place":{"file":"","offset":0}}]},` +
	`{"place":{"file":"shape.go","offset":112},"kind":"const","name":"kept","owners":[{"kind":"function",` +
	`"local":0,"place":{"file":"","offset":0}}]},{"place":{"file":"shape.go","offset":141},"kind":"field",` +
	`"name":"field","owners":[{"kind":"local","local":0,"place":{"file":"","offset":0}}]}],` +
	`"uses":[{"by":{"kind":"function",` +
	`"local":0,"place":{"file":"","offset":0}},"used":{"kind":"declared","local":0,"place":{"file":"shape.go",` +
	`"offset":20}}},{"by":{"kind":"function","local":0,"place":{"file":"","offset":0}},"used":{"kind":"local",` +
	`"local":0,"place":{"file":"","offset":0}}},{"by":{"kind":"local","local":2,"place":{"file":"",` +
	`"offset":0}},"used":{"kind":"declared","local":0,"place":{"file":"shape.go","offset":34}}},` +
	`{"by":{"kind":"declared","local":0,"place":{"file":"shape.go","offset":34}},"used":{"kind":"local",` +
	`"local":2,"place":{"file":"","offset":0}}},{"by":{"kind":"function","local":0,"place":{"file":"",` +
	`"offset":0}},"used":{"kind":"declared","local":0,"place":{"file":"shape.go","offset":34}}}],` +
	`"rooted":[1],"files":["shape.go"],"present":{"unexportedInterfaceMethod":false,"localMethodSet":false,` +
	`"interfaceInstance":false,"ignoredType":false,"allFieldsUsed":false,"unplacedEnd":false}}`

// TestABodyRecordEncodesAsItAlwaysHas holds encoded to the exact bytes of a record whose body fills each of its parts
// (#xeafx79): the compact form a run holds spells back out to the canonical form, unchanged, and the canonical form
// is the one a reader of the record has always read.
func TestABodyRecordEncodesAsItAlwaysHas(test *testing.T) {
	test.Parallel()
	checked := checkShapes(test, "golden", goldenSources, nil)
	recording, recordError := checked.build(true, nil)
	if recordError != nil {
		test.Fatal(recordError)
	}
	record, isRecorded := recordKeyedBy(recording.bodyRecords(), functionBodyKey(test, checked, "Use"))
	if !isRecorded {
		test.Fatal("the recorder wrote no one record of Use's body")
	}
	encoded, encodeError := record.encoded()
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if string(encoded) != goldenRecord {
		test.Errorf("Use's record encodes as\n%s\nnot as\n%s", encoded, goldenRecord)
	}
}
