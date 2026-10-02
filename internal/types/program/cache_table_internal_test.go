package program

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var internalTestIdentity = CacheTableIdentity{SelfCommit: "test", CompilerCommit: "test", GoToolchain: "go-test", Platform: "test/test"}

// encodeRaw writes a header and a body exactly as given, so a test can produce what no current encoder
// would: an old version, a wrong magic, a list index past its table.
func encodeRaw(t *testing.T, header cacheTableHeader, body cacheTableBody) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(header); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(body); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func currentHeader() cacheTableHeader {
	return cacheTableHeader{Magic: cacheTableMagic, Version: cacheTableVersion, Identity: internalTestIdentity}
}

// TestCacheTableRefusesAnotherFormat covers the two header fields no other build of this code can get
// wrong, and so only a raw write can: whatever the version is now, never a number someone must remember
// to update, and the magic.
func TestCacheTableRefusesAnotherFormat(t *testing.T) {
	for name, header := range map[string]cacheTableHeader{
		"an older version": {Magic: cacheTableMagic, Version: cacheTableVersion - 1, Identity: internalTestIdentity},
		"a newer version":  {Magic: cacheTableMagic, Version: cacheTableVersion + 1, Identity: internalTestIdentity},
		"another magic":    {Magic: "something else", Version: cacheTableVersion, Identity: internalTestIdentity},
	} {
		t.Run(name, func(t *testing.T) {
			buffer := encodeRaw(t, header, cacheTableBody{Runs: map[string]*RunCache{"": {Key: "k"}}})
			if _, err := DecodeCacheTable(buffer, internalTestIdentity); !errors.Is(err, ErrCacheTableUnreadable) {
				t.Fatalf("decoded %s: %v", name, err)
			}
		})
	}
}

// TestCacheTableRefusesAListIndexPastItsTable pins the one check gob cannot make for us. The rule lists
// are interned by hand, and an index trusted past its table would hand an entry another file's rules.
func TestCacheTableRefusesAListIndexPastItsTable(t *testing.T) {
	for _, field := range []string{"Rules", "TypedRules", "Listening", "ShapedRules"} {
		t.Run(field, func(t *testing.T) {
			entry := lintCacheWireEntry{Path: "/a.ts"}
			reflect.ValueOf(&entry).Elem().FieldByName(field).SetInt(1)
			body := cacheTableBody{Findings: &lintCacheWire{
				Version: lintCacheVersion,
				Lists:   [][]string{{"no-debugger"}},
				Entries: []lintCacheWireEntry{entry},
			}}
			if _, err := DecodeCacheTable(encodeRaw(t, currentHeader(), body), internalTestIdentity); !errors.Is(err, ErrCacheTableUnreadable) {
				t.Fatalf("an entry whose %s index is past the list table decoded: %v", field, err)
			}
		})
	}
}

// TestCacheTableDropsAFindingsSectionOfAnotherMeaning: the findings section keeps its own version, for
// changes in what an entry means rather than in its shape. One of another version is dropped, which is a
// miss, and the runs beside it are kept, since each is proven by its own key.
func TestCacheTableDropsAFindingsSectionOfAnotherMeaning(t *testing.T) {
	body := cacheTableBody{
		Runs:     map[string]*RunCache{"--no-fix": {Key: "kept"}},
		Findings: &lintCacheWire{Version: lintCacheVersion - 1, Lists: [][]string{{}}, Entries: []lintCacheWireEntry{{Path: "/a.ts"}}},
	}
	table, err := DecodeCacheTable(encodeRaw(t, currentHeader(), body), internalTestIdentity)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if table.Findings != nil {
		t.Error("a findings section of another version was used")
	}
	if table.Runs["--no-fix"] == nil {
		t.Error("the runs were thrown away with the findings")
	}
}

// pinnedCacheTableShapes is the encoded shape each format version stands for. gob decodes a renamed,
// added or removed field without error, into a zero value, so a shape that changes under an unchanged
// version is a silent partial read of every old table. Changing a field means bumping cacheTableVersion
// and pinning the new shape under it, and this test says so when it is forgotten.
var pinnedCacheTableShapes = map[int]string{
	1: "3db631dcfd27db51bf5c9e29a4a0ac41a7ba30dc90aeb8712037e057a516effb",
	2: "30d879299d079a880679687f46936674d9ecd124a5916a1ea76ff210d6e67873",
	3: "c01be7eecead0ab5c95964ed45cb149bcfd0883b2130a21e39d8d25e3c1d3622",
}

func TestCacheTableShapeIsPinnedToItsVersion(t *testing.T) {
	shape := describeShape(reflect.TypeOf(cacheTableHeader{})) + "\n" + describeShape(reflect.TypeOf(cacheTableBody{}))
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(shape)))
	pinned, found := pinnedCacheTableShapes[cacheTableVersion]
	if !found || pinned != sum {
		t.Fatalf("the cache table's encoded shape is %s under version %d, pinned as %q.\n"+
			"If a field changed, bump cacheTableVersion and pin %s under the new version: an old table would "+
			"otherwise decode into zero values without an error.\nThe shape:\n%s",
			sum, cacheTableVersion, pinned, sum, shape)
	}
}

// describeShape renders a type the way gob sees it: exported fields by name and type, recursively.
func describeShape(subject reflect.Type) string {
	var out strings.Builder
	var walk func(reflect.Type, map[reflect.Type]bool)
	walk = func(subject reflect.Type, seen map[reflect.Type]bool) {
		switch subject.Kind() {
		case reflect.Pointer:
			out.WriteString("*")
			walk(subject.Elem(), seen)
		case reflect.Slice:
			out.WriteString("[]")
			walk(subject.Elem(), seen)
		case reflect.Array:
			fmt.Fprintf(&out, "[%d]", subject.Len())
			walk(subject.Elem(), seen)
		case reflect.Map:
			out.WriteString("map[")
			walk(subject.Key(), seen)
			out.WriteString("]")
			walk(subject.Elem(), seen)
		case reflect.Struct:
			if seen[subject] {
				out.WriteString(subject.Name())
				return
			}
			seen[subject] = true
			out.WriteString(subject.Name() + "{")
			for index := range subject.NumField() {
				field := subject.Field(index)
				if !field.IsExported() {
					continue
				}
				out.WriteString(field.Name + " ")
				walk(field.Type, seen)
				out.WriteString("; ")
			}
			out.WriteString("}")
		default:
			out.WriteString(subject.Kind().String())
		}
	}
	walk(subject, map[reflect.Type]bool{})
	return out.String()
}
