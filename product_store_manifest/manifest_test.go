package product_store_manifest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/product_store_manifest"
)

// The oracle is the bytes each side encodes today, held as goldens in testdata:
//
//   - loom.json and empty.json are what Loom's HeldProducts.prepare writes (builder/held.go at
//     cc23fbe41): a product of two files, and one with none, whose list Loom starts as []heldFile{}.
//     Name is never set there, so it encodes "".
//   - adamic.json and adamic_empty.json are what Adamic's buildcache wrote through its manifest type
//     (internal/buildcache/store.go at a23ba09638), as its store test's fake still does: Name set to
//     the fake's, and an empty product's Files left nil, so null.
//
// Neither side's struct can be imported from here, so each is copied below as it reads at that
// commit, and TestTheGoldensAreWhatEachSideEncodes holds the goldens to them. The module's own types
// are then held to the goldens, never to themselves.

// loomHeldManifest and loomHeldFile are loom's heldManifest and heldFile, held.go:31-43: the same
// fields, types, order and tags, with the house's spelling of the Go name Sha256, which the bytes
// don't see.
type loomHeldManifest struct {
	Version int            `json:"version"`
	Key     string         `json:"key"`
	Name    string         `json:"name"`
	Files   []loomHeldFile `json:"files"`
}

type loomHeldFile struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

// adamicManifest and adamicManifestFile are adamic's manifest and manifestFile, store.go:54-66, copied
// as loom's are.
type adamicManifest struct {
	Version int                  `json:"version"`
	Key     string               `json:"key"`
	Name    string               `json:"name"`
	Files   []adamicManifestFile `json:"files"`
}

type adamicManifestFile struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

// productKey is loom's held_test sharedProduct.
var productKey = strings.Repeat("5", 64)

// adamicName is the name adamic's store test fake gives every product it stores (store_test.go:59).
const adamicName = "buildcache test"

// The fixture product's two files, as TestDescribeWalksAProductAsLoomDoes writes them. Their sums were
// taken with `printf '<content>' | shasum -a 256`, outside Go.
const (
	toolContent   = "the tool"
	toolSum       = "6866d28d44ec66b4dc63a7183b503b39881d59d928c53edff3c5096853a659e4"
	objectContent = "object"
	objectSum     = "2958d416d08aa5a472d7b509036cb7eafd542add84527e66a145ea64cb4cdc75"
)

// The sha256 of each Loom golden's bytes, taken with `shasum -a 256 testdata/<name>`: what Loom's
// digest(encoded) (held.go:231) serves as the body of refs/build/<key>.
const (
	loomManifestSum  = "a0b6c59dd26076ed2a327b5cb9b99899383ea171251598a1d3590430c41b1f34"
	emptyManifestSum = "2ff53299f4330c7f55646e4349d04cc32cd979693256c6a40465c0bbe1b3a604"
)

// fixtureFiles is the fixture product's files in the module's types, sorted by path.
func fixtureFiles() []product_store_manifest.File {
	return []product_store_manifest.File{
		{Path: "bin/tool", Sha256: toolSum, Size: int64(len(toolContent)), Mode: 0o755},
		{Path: "lib/a.o", Sha256: objectSum, Size: int64(len(objectContent)), Mode: 0o644},
	}
}

// fixture is a version 1 manifest of productKey with this name and these files.
func fixture(name string, files []product_store_manifest.File) product_store_manifest.Manifest {
	return product_store_manifest.Manifest{
		Version: product_store_manifest.Version, Key: productKey, Name: name, Files: files,
	}
}

func golden(test *testing.T, name string) []byte {
	test.Helper()
	content, readError := os.ReadFile(filepath.Join("testdata", name))
	if readError != nil {
		test.Fatal(readError)
	}
	return content
}

func encode(test *testing.T, manifest product_store_manifest.Manifest) []byte {
	test.Helper()
	encoded, encodeError := manifest.Encode()
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return encoded
}

// loomDigest is loom's digest (builder/store.go:121), the hash held.go:231 takes of the encoded manifest.
func loomDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// The goldens are each side's bytes: json.Marshal of loom's and adamic's own structs, filled as each
// side fills them, reproduces every golden byte for byte. A golden edited by hand fails here.
func TestTheGoldensAreWhatEachSideEncodes(test *testing.T) {
	test.Parallel()
	loomFiles := []loomHeldFile{
		{Path: "bin/tool", Sha256: toolSum, Size: int64(len(toolContent)), Mode: 0o755},
		{Path: "lib/a.o", Sha256: objectSum, Size: int64(len(objectContent)), Mode: 0o644},
	}
	adamicFiles := []adamicManifestFile{
		{Path: "bin/tool", Sha256: toolSum, Size: int64(len(toolContent)), Mode: 0o755},
		{Path: "lib/a.o", Sha256: objectSum, Size: int64(len(objectContent)), Mode: 0o644},
	}
	for name, value := range map[string]any{
		"loom.json":         loomHeldManifest{Version: 1, Key: productKey, Files: loomFiles},
		"empty.json":        loomHeldManifest{Version: 1, Key: productKey, Files: []loomHeldFile{}},
		"adamic.json":       adamicManifest{Version: 1, Key: productKey, Name: adamicName, Files: adamicFiles},
		"adamic_empty.json": adamicManifest{Version: 1, Key: productKey, Name: adamicName},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			encoded, marshalError := json.Marshal(value)
			if marshalError != nil {
				test.Fatal(marshalError)
			}
			if want := golden(test, name); string(encoded) != string(want) {
				test.Fatalf("its side encodes\n%s\nthe golden holds\n%s", encoded, want)
			}
		})
	}
}

// Encode writes Loom's bytes exactly, for a product with files and for an empty one, whether its Files
// is empty or nil: no caller can write "files":null.
func TestEncodeWritesLoomsBytes(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		manifest product_store_manifest.Manifest
		golden   string
	}{
		"two files":       {fixture("", fixtureFiles()), "loom.json"},
		"no files":        {fixture("", []product_store_manifest.File{}), "empty.json"},
		"nil files":       {fixture("", nil), "empty.json"},
		"adamic's fields": {fixture(adamicName, fixtureFiles()), "adamic.json"},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			encoded := encode(test, testCase.manifest)
			if want := golden(test, testCase.golden); string(encoded) != string(want) {
				test.Fatalf("Encode writes\n%s\n%s holds\n%s", encoded, testCase.golden, want)
			}
		})
	}
}

// The ref's body is the sha256 of the encoded bytes: Loom's digest of Encode's output is the sum of the
// golden file, taken outside Go, for the fixture and for the empty product.
func TestTheRefNamesTheSumOfLoomsBytes(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		manifest product_store_manifest.Manifest
		sum      string
	}{
		"two files": {fixture("", fixtureFiles()), loomManifestSum},
		"no files":  {fixture("", nil), emptyManifestSum},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if sum := loomDigest(encode(test, testCase.manifest)); sum != testCase.sum {
				test.Fatalf("the ref would name %s, Loom's bytes hash to %s", sum, testCase.sum)
			}
		})
	}
}

// Decode reads both sides' bytes into the same value: adamic's differ from Loom's only in the name it
// set, and its null list reads as Loom's empty one, never nil.
func TestDecodeReadsBothSidesToTheSameValue(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		golden string
		want   product_store_manifest.Manifest
		name   string
	}{
		"loom's two files":   {"loom.json", fixture("", fixtureFiles()), ""},
		"adamic's two files": {"adamic.json", fixture("", fixtureFiles()), adamicName},
		"loom's empty":       {"empty.json", fixture("", []product_store_manifest.File{}), ""},
		"adamic's null":      {"adamic_empty.json", fixture("", []product_store_manifest.File{}), adamicName},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeError := product_store_manifest.Decode(golden(test, testCase.golden), productKey)
			if decodeError != nil {
				test.Fatal(decodeError)
			}
			if decoded.Name != testCase.name {
				test.Fatalf("%s reads the name %q, want %q", testCase.golden, decoded.Name, testCase.name)
			}
			// Apart from the name each side gave it, the value is Loom's. reflect.DeepEqual tells a nil
			// list from an empty one, so null reading as nil fails here.
			decoded.Name = ""
			if !reflect.DeepEqual(decoded, testCase.want) {
				test.Fatalf("%s reads as %#v, want %#v", testCase.golden, decoded, testCase.want)
			}
		})
	}
}

// Decode refuses what buildcache's fetch refused on read (store.go:230-235): another version, another
// key, and every path a reader would write outside its product. A name that starts with dots but stays
// inside is a path like any other.
func TestDecodeRefusesAManifestThatDoesntCheck(test *testing.T) {
	test.Parallel()
	withPath := func(path string) product_store_manifest.Manifest {
		files := fixtureFiles()
		files[0].Path = path
		return fixture("", files)
	}
	refused := map[string]func(test *testing.T) []byte{
		"not JSON":   func(*testing.T) []byte { return []byte("latest") },
		"no version": func(*testing.T) []byte { return []byte(`{"key":"` + productKey + `","files":[]}`) },
		"another version": func(test *testing.T) []byte {
			return encode(test, product_store_manifest.Manifest{Version: 2, Key: productKey})
		},
		"another key": func(test *testing.T) []byte {
			return encode(test, product_store_manifest.Manifest{
				Version: product_store_manifest.Version, Key: strings.Repeat("0", 64),
			})
		},
	}
	for _, path := range []string{
		"../escape", "..", "/etc/passwd", ".", "", "bin/../tool", "bin//tool", "bin/tool/", "./bin/tool", "bin/./tool",
	} {
		refused["the path "+path] = func(test *testing.T) []byte { return encode(test, withPath(path)) }
	}
	for name, content := range refused {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeError := product_store_manifest.Decode(content(test), productKey)
			if decodeError == nil {
				test.Fatalf("decoded %#v, want a refusal", decoded)
			}
		})
	}
	for _, path := range []string{"..tool", "bin/..tool", "...", "bin/tool..", `bin\tool`} {
		test.Run("the path "+path+" is inside", func(test *testing.T) {
			test.Parallel()
			_, decodeError := product_store_manifest.Decode(encode(test, withPath(path)), productKey)
			if decodeError != nil {
				test.Fatal(decodeError)
			}
		})
	}
}

// write makes a file under root with exactly this mode, whatever the umask.
func write(test *testing.T, root, name, content string, mode fs.FileMode) {
	test.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if makeError := os.MkdirAll(filepath.Dir(file), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(file, []byte(content), mode); writeError != nil {
		test.Fatal(writeError)
	}
	if chmodError := os.Chmod(file, mode); chmodError != nil {
		test.Fatal(chmodError)
	}
}

// Describe walks a product as Loom's prepare does: the fixture's files on disk encode as loom.json, byte
// for byte, and Decode reads that back as the manifest Describe made.
func TestDescribeWalksAProductAsLoomDoes(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	write(test, root, "lib/a.o", objectContent, 0o640)
	write(test, root, "bin/tool", toolContent, 0o700)
	described, describeError := product_store_manifest.Describe(productKey, "", root)
	if describeError != nil {
		test.Fatal(describeError)
	}
	encoded := encode(test, described)
	if want := golden(test, "loom.json"); string(encoded) != string(want) {
		test.Fatalf("Describe encodes\n%s\nloom.json holds\n%s", encoded, want)
	}
	decoded, decodeError := product_store_manifest.Decode(encoded, productKey)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	if !reflect.DeepEqual(decoded, described) {
		test.Fatalf("Decode reads %#v back as %#v", described, decoded)
	}
}

// Files are sorted by path, not in the walk's order: "a.txt" before "a/b", though the walk visits a/
// first, since "." sorts before "/".
func TestDescribeSortsByPath(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	for _, name := range []string{"a/b", "a.txt", "a/c/d", "b"} {
		write(test, root, name, name, 0o644)
	}
	described, describeError := product_store_manifest.Describe(productKey, "", root)
	if describeError != nil {
		test.Fatal(describeError)
	}
	var paths []string
	for _, file := range described.Files {
		paths = append(paths, file.Path)
	}
	if want := []string{"a.txt", "a/b", "a/c/d", "b"}; !reflect.DeepEqual(paths, want) {
		test.Fatalf("Describe lists %q, want %q", paths, want)
	}
}

// A missing root is fs.ErrNotExist beside the empty product's manifest, which Loom keeps; an empty
// directory is that manifest with no error. Either encodes as empty.json.
func TestDescribeOfAnEmptyOrMissingProduct(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		root    func(test *testing.T) string
		missing bool
	}{
		"empty":   {func(test *testing.T) string { return test.TempDir() }, false},
		"missing": {func(test *testing.T) string { return filepath.Join(test.TempDir(), productKey) }, true},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			described, describeError := product_store_manifest.Describe(productKey, "", testCase.root(test))
			missing := errors.Is(describeError, fs.ErrNotExist)
			if missing != testCase.missing || (describeError != nil && !missing) {
				test.Fatalf("Describe gave %v", describeError)
			}
			if encoded, want := encode(test, described), golden(test, "empty.json"); string(encoded) != string(want) {
				test.Fatalf("Describe encodes\n%s\nempty.json holds\n%s", encoded, want)
			}
		})
	}
}

// What can't be stored is an error, never a manifest that leaves it out: a symlink in the product, and a
// root that is itself a file or a link. The manifest beside the error is the empty one, never a part.
func TestDescribeRefusesWhatCantBeStored(test *testing.T) {
	test.Parallel()
	for name, root := range map[string]func(test *testing.T) string{
		"a symlink inside": func(test *testing.T) string {
			root := test.TempDir()
			write(test, root, "bin/tool", toolContent, 0o755)
			if symlinkError := os.Symlink("tool", filepath.Join(root, "bin", "link")); symlinkError != nil {
				test.Fatal(symlinkError)
			}
			return root
		},
		"a file for a root": func(test *testing.T) string {
			root := test.TempDir()
			write(test, root, "tool", toolContent, 0o755)
			return filepath.Join(root, "tool")
		},
		"a link for a root": func(test *testing.T) string {
			directory := test.TempDir()
			write(test, directory, "product/bin/tool", toolContent, 0o755)
			link := filepath.Join(directory, "link")
			if symlinkError := os.Symlink(filepath.Join(directory, "product"), link); symlinkError != nil {
				test.Fatal(symlinkError)
			}
			return link
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			described, describeError := product_store_manifest.Describe(productKey, "", root(test))
			if describeError == nil {
				test.Fatalf("described %#v, want a refusal", described)
			}
			if len(described.Files) != 0 || described.Files == nil {
				test.Fatalf("beside %v: %#v, want the empty product", describeError, described.Files)
			}
		})
	}
}

// Mode keeps only whether anyone may execute a file, so a umask or a setuid bit never reaches a manifest.
func TestModeKeepsOnlyWhetherAnyoneMayExecute(test *testing.T) {
	test.Parallel()
	for mode, want := range map[fs.FileMode]uint32{
		0o644:                 0o644,
		0o600:                 0o644,
		0o666:                 0o644,
		0o000:                 0o644,
		0o644 | fs.ModeSticky: 0o644,
		0o700:                 0o755,
		0o744:                 0o755,
		0o010:                 0o755,
		0o001:                 0o755,
		0o777:                 0o755,
		0o755 | fs.ModeSetuid: 0o755,
	} {
		if got := product_store_manifest.Mode(mode); got != want {
			test.Errorf("Mode(%v) is %#o, want %#o", mode, got, want)
		}
	}
}
