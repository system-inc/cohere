package consistency_require_matching_file_name

import "testing"

// A file named for a build target is left out of a package built for another, so analysistest on any one machine
// can load at most one target's fixtures. The names are judged here instead, for every suffix go/build reads and
// for the ones it does not, so the toolchain's grammar is proven whatever machine runs the test.
func TestNamesType(test *testing.T) {
	test.Parallel()
	cases := []struct {
		stem      string
		typeName  string
		declared  bool
		wantNamed bool
	}{
		{stem: "registry", typeName: "Registry", declared: true, wantNamed: true},
		{stem: "registry_linux", typeName: "Registry", declared: true, wantNamed: true},
		{stem: "registry_arm64", typeName: "Registry", declared: true, wantNamed: true},
		{stem: "registry_linux_amd64", typeName: "Registry", declared: true, wantNamed: true},
		{stem: "http_server_windows", typeName: "HttpServer", declared: true, wantNamed: true},
		{stem: "base64_encoder", typeName: "Base64Encoder", declared: true, wantNamed: true},
		{stem: "base_64_encoder", typeName: "Base64Encoder", declared: true, wantNamed: true},
		{stem: "größe", typeName: "Größe", declared: true, wantNamed: true},
		// A type whose own name ends in a target's word is named for whole.
		{stem: "poller_linux", typeName: "PollerLinux", declared: true, wantNamed: true},
		// An architecture before an operating system is no suffix go/build reads, so the name is not the type's.
		{stem: "registry_amd64_linux", typeName: "Registry", declared: true, wantNamed: false},
		{stem: "registry_helpers", typeName: "Registry", declared: true, wantNamed: false},
		{stem: "registry_linux_helpers", typeName: "Registry", declared: true, wantNamed: false},
		{stem: "linux", typeName: "Registry", declared: true, wantNamed: false},
		{stem: "registries", typeName: "Registry", declared: true, wantNamed: false},
		{stem: "registry_helpers", typeName: "Registry", declared: false, wantNamed: true},
		{stem: "registry_helpers_linux", typeName: "Registry", declared: false, wantNamed: true},
		{stem: "helpers_registry", typeName: "Registry", declared: false, wantNamed: false},
		// A trailing underscore names no theme, and the stem still spells the type.
		{stem: "registry_", typeName: "Registry", declared: true, wantNamed: true},
	}
	for _, testCase := range cases {
		allowedTail := isToolchainSuffix
		if !testCase.declared {
			allowedTail = isThemeSuffix
		}
		if named := namesType(testCase.stem, testCase.typeName, allowedTail); named != testCase.wantNamed {
			test.Errorf(
				"namesType(%q, %q), declared %v, is %v, want %v",
				testCase.stem, testCase.typeName, testCase.declared, named, testCase.wantNamed,
			)
		}
	}
}

// The name a finding suggests keeps the file's build target, so following it never changes what the file builds
// for, and always passes the rule.
func TestSuggestedName(test *testing.T) {
	test.Parallel()
	cases := []struct {
		typeName string
		stem     string
		want     string
	}{
		{typeName: "Registry", stem: "store", want: "registry"},
		{typeName: "Registry", stem: "store_linux", want: "registry_linux"},
		{typeName: "Registry", stem: "store_linux_arm64", want: "registry_linux_arm64"},
		{typeName: "Registry", stem: "store_helpers_wasm", want: "registry_wasm"},
		{typeName: "Registry", stem: "linux", want: "registry"},
		{typeName: "HttpServer", stem: "server", want: "http_server"},
		{typeName: "URLParser", stem: "parse", want: "url_parser"},
		{typeName: "parseState", stem: "state", want: "parse_state"},
		{typeName: "Utf8Reader", stem: "reader", want: "utf8_reader"},
	}
	for _, testCase := range cases {
		suggestion := snakeCase(testCase.typeName) + toolchainSuffix(testCase.stem)
		if suggestion != testCase.want {
			test.Errorf(
				"the suggestion for %s in %s is %q, want %q",
				testCase.typeName, testCase.stem, suggestion, testCase.want,
			)
		}
		if !namesType(suggestion, testCase.typeName, isToolchainSuffix) {
			test.Errorf("the suggestion %q for %s does not pass the rule", suggestion, testCase.typeName)
		}
	}
}
