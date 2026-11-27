package tailwind

import "testing"

// tailwindSystems are the design systems every assertion about Tailwind runs on (#f598zk0): the
// independent theme over the vendored tailwindcss, on every machine and on CI. A design system committed
// beside it joins every assertion by joining this list.
var tailwindSystems = []struct {
	name string
	load func(test *testing.T) *LoadedDesignSystem
}{
	{name: "public", load: unseenDesignSystem},
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
