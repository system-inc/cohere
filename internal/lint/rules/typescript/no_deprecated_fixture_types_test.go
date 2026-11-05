package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// fixtureReactTypes is a synthetic @types package, the least of @types/react that the JSX case in
// TestNoDeprecatedInJsx reads through: a generic component alias and the JSX namespace a <div> needs. It
// proves the same mechanism as that case, a prop marked @deprecated, reached through a generic component
// alias an installed package declares, on any machine, with nothing vendored. The tag is on the source's
// own Props, as in upstream's invalid:145; the package supplies only the alias. The live case against the
// real @types/react stays beside it, opt-in through the ahra corpus (#sycrdr6).
const fixtureReactTypes = `export type FC<P> = (props: P) => unknown;

declare global {
	namespace JSX {
		interface Element {}
		interface IntrinsicElements {
			div: {};
		}
	}
}
`

// fixtureReactSource is invalid:145's source with its import pointed at the synthetic package.
const fixtureReactSource = `
import * as React from 'fixture-react';

interface Props {
  /**
   * @deprecated
   */
  deprecatedProp: string;
}

interface Tab {
  List: React.FC<Props>;
}

const Tab: Tab = {
  List: () => <div>Hi</div>,
};

const anotherExample = <Tab.List deprecatedProp="oh no" />;
`

// installFixtureReact writes the synthetic package into a fixture's node_modules/@types.
func installFixtureReact(test *testing.T, types string) func(string) {
	test.Helper()
	return func(directory string) {
		packageDirectory := filepath.Join(directory, "node_modules", "@types", "fixture-react")
		if createDirectoryError := os.MkdirAll(packageDirectory, 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		files := map[string]string{
			"package.json": `{"name": "@types/fixture-react", "version": "0.0.0", "types": "index.d.ts"}`,
			"index.d.ts":   types,
		}
		for name, contents := range files {
			if writeError := os.WriteFile(
				filepath.Join(packageDirectory, name),
				[]byte(contents),
				0o644,
			); writeError != nil {
				test.Fatal(writeError)
			}
		}
	}
}

// A prop marked @deprecated, reached through a generic component alias an installed package declares,
// is reported, on any machine.
func TestNoDeprecatedReadsAPropThroughAnInstalledComponentAlias(test *testing.T) {
	test.Parallel()
	subject := "/repository/source/Subject.tsx"
	result := rule_testing.RunTypedFilesWithSetup(test, NoDeprecated,
		map[string]string{subject: fixtureReactSource}, subject, installFixtureReact(test, fixtureReactTypes))
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Message.Description != "`deprecatedProp` is deprecated." {
		test.Fatalf("want the one deprecatedProp finding, got %s", describeNoDeprecated(result))
	}
}

// The control: the same source with the @deprecated tag removed reports nothing, so the finding above is
// the tag's, read through the package, and not something else about the fixture.
func TestNoDeprecatedReportsNothingWithoutTheTag(test *testing.T) {
	test.Parallel()
	subject := "/repository/source/Subject.tsx"
	untagged := strings.Replace(fixtureReactSource, "   * @deprecated\n", "", 1)
	if untagged == fixtureReactSource {
		test.Fatal("the source no longer carries the @deprecated tag this control removes")
	}
	result := rule_testing.RunTypedFilesWithSetup(test, NoDeprecated,
		map[string]string{subject: untagged}, subject, installFixtureReact(test, fixtureReactTypes))
	if len(result.Diagnostics) != 0 {
		test.Fatalf("want no findings without the tag, got %s", describeNoDeprecated(result))
	}
}
