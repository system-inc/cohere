package policy

import (
	"strings"
	"testing"
)

// minimalBrands is a file every refusal below starts from, changed in one place, so each refusal is the one under
// test rather than some other.
const minimalBrands = `{"about": "a", "brands": [` +
	`{"brand": "iMessage", "owner": "Apple", "citation": "https://example.com/imessage"}, ` +
	`{"brand": "iMessageKit", "owner": "Apple", "citation": "https://example.com/imessagekit"}]}`

// The minimal file loads and reads a name by the longest brand it opens with, and each one-place change that would
// misread, or would admit an entry the bar refuses, is refused.
func TestBrandsLoaderRefusesWhatWouldReadAsSomethingElse(test *testing.T) {
	test.Parallel()
	vocabulary, loadError := loadBrands([]byte(minimalBrands))
	if loadError != nil {
		test.Fatalf("the minimal file is refused (%v), so no refusal below would mean anything", loadError)
	}
	for _, read := range []struct {
		name  string
		brand string
	}{
		{"iMessageThing", "iMessage"},
		{"iMessage", "iMessage"},
		{"iMessageKitThing", "iMessageKit"},
		{"IMessageThing", ""},
		{"imessageThing", ""},
		{"SendiMessage", ""},
	} {
		brand, found := vocabulary.LeadingBrand(read.name)
		if brand != read.brand || found != (read.brand != "") {
			test.Errorf("%s opens with %q (found %t), want %q", read.name, brand, found, read.brand)
		}
	}
	for _, refused := range []struct {
		name        string
		original    string
		replacement string
	}{
		{"an unknown top-level key", `"about": "a"`, `"about": "a", "aliases": []`},
		{"an unknown key on an entry", `"owner": "Apple", "citation": "https://example.com/imessage"`,
			`"owner": "Apple", "note": "n", "citation": "https://example.com/imessage"`},
		{"an empty about", `"about": "a"`, `"about": " "`},
		{"a duplicate", `"brand": "iMessageKit"`, `"brand": "iMessage"`},
		{"a brand opening with a capital", `"brand": "iMessage"`, `"brand": "IMessage"`},
		{"a brand opening with a digit", `"brand": "iMessage"`, `"brand": "1Message"`},
		{"an empty brand", `"brand": "iMessage"`, `"brand": ""`},
		{"a brand with a space", `"brand": "iMessage"`, `"brand": "i Message"`},
		{"a brand with a hyphen", `"brand": "iMessage"`, `"brand": "i-Message"`},
		{"an entry with no owner", `"owner": "Apple", "citation": "https://example.com/imessage"`,
			`"citation": "https://example.com/imessage"`},
		{"an entry with no citation", `, "citation": "https://example.com/imessage"`, ``},
		{"an empty citation", `"https://example.com/imessage"`, `""`},
		{"a citation that is not an address", `"https://example.com/imessage"`, `"Apple's trademark list"`},
		{"a citation over plain http", `"https://example.com/imessage"`, `"http://example.com/imessage"`},
	} {
		changed := strings.Replace(minimalBrands, refused.original, refused.replacement, 1)
		if changed == minimalBrands {
			test.Fatalf("%s: the change did not apply, so it would test nothing", refused.name)
		}
		if _, refusedError := loadBrands([]byte(changed)); refusedError == nil {
			test.Errorf("%s is accepted: %s", refused.name, changed)
		}
	}
}

// The embedded file loads, which the package's init already proves by not panicking, and holds iMessage, the brand
// the list was opened for, so a file trimmed by mistake is caught here rather than as a finding come back.
func TestBrandsHoldsIMessage(test *testing.T) {
	test.Parallel()
	if brand, found := Brands.LeadingBrand("iMessageCommandLineInterface"); !found || brand != "iMessage" {
		test.Errorf("the embedded vocabulary reads iMessageCommandLineInterface as opening with %q", brand)
	}
}
