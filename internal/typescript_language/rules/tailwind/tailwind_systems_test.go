package tailwind

import "testing"

// tailwindSystem is one design system an assertion about Tailwind runs on.
type tailwindSystem struct {
	name string
	load func(test *testing.T) DesignSystemResult
}

// tailwindSystems are the design systems every assertion about Tailwind runs on (#f598zk0).
//
// The public one is the independent theme in this package's testdata over the vendored tailwindcss,
// so the assertion runs on every machine and on CI. ahra's is the live one these tests were written
// against, opt-in through its corpus: a synthetic theme proves Tailwind on the shapes we thought of,
// and the real one is where the shapes we did not think of live.
var tailwindSystems = []tailwindSystem{
	{name: "public", load: publicDesignSystem},
	{name: "ahra", load: classOrderLiveRepositorySystem},
}

// forEachTailwindSystem runs Tailwind assertion once per design system, each as its own subtest, so a
// machine without the ahra corpus skips that subtest by name and still runs the public one.
func forEachTailwindSystem(test *testing.T, run func(test *testing.T, designSystem DesignSystemResult)) {
	test.Helper()
	for _, system := range tailwindSystems {
		test.Run(system.name, func(test *testing.T) {
			test.Parallel()
			run(test, system.load(test))
		})
	}
}

// publicDesignSystem is the independent theme's design system, with its table, in the shape the live
// helpers return.
func publicDesignSystem(test *testing.T) DesignSystemResult {
	test.Helper()
	system := independentLiveSystem(test)
	return loadedDesignSystemResult(system, "")
}
