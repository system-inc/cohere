package tailwind

import "testing"

// tailwindSystems are the design systems every assertion about Tailwind runs on (#f598zk0), each over the
// vendored tailwindcss, on every machine and on CI: the independent theme, and the planted repository's
// theme (#pg78xtp), so an engine assertion runs on two themes that differ, as it once ran on two consumer
// repositories, and a fact true of one theme alone does not pass as a fact about the engine. A design
// system committed beside them joins every assertion by joining this list.
var tailwindSystems = []struct {
	name string
	load func(test *testing.T) *LoadedDesignSystem
}{
	{name: "public", load: unseenDesignSystem},
	{name: "planted", load: plantedThemeSystem},
}

// forEachTailwindSystem runs Tailwind assertion once per design system, each as its own subtest.
func forEachTailwindSystem(test *testing.T, run func(test *testing.T, system *LoadedDesignSystem)) {
	test.Helper()
	for _, tailwindSystem := range tailwindSystems {
		test.Run(tailwindSystem.name, func(test *testing.T) {
			test.Parallel()
			run(test, tailwindSystem.load(test))
		})
	}
}
