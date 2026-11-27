package tailwind

import "testing"

// tailwindSystem is one design system an assertion about Tailwind runs on.
type tailwindSystem struct {
	name string
	load func(test *testing.T) DesignSystemResult
}

// tailwindSystems are the design systems every assertion about Tailwind runs on (#f598zk0): the independent
// theme in this package's testdata over the vendored tailwindcss, so the assertion runs on every machine and on
// CI, and the planted repository's theme (#pg78xtp), which overrides `dark`, adds variants and breakpoints of its
// own and declares the root shapes a repository writes, the place the consumer theme these assertions once also
// ran on had shapes nobody constructed. A design system committed beside them joins every assertion by joining
// this list.
var tailwindSystems = []tailwindSystem{
	{name: "public", load: publicDesignSystem},
	{name: "planted", load: plantedDesignSystem},
}

// forEachTailwindSystem runs Tailwind assertion once per design system, each as its own subtest.
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

// plantedDesignSystem is the planted repository's design system, over the vendored tailwindcss.
func plantedDesignSystem(test *testing.T) DesignSystemResult {
	test.Helper()
	return livePlacementSystem(
		test,
		"internal/lint/rules/tailwind/collapse/testdata/planted_repository/styles/theme.css",
	)
}
