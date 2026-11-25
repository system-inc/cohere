package tailwind

import "testing"

// tailwindSystems are the design systems every assertion about Tailwind runs on (#f598zk0): the
// independent theme over the vendored tailwindcss, on every machine and on CI, and ahra's, the live
// one these tests were written against, opt-in through its corpus.
var tailwindSystems = []struct {
	name string
	load func(test *testing.T) *LoadedDesignSystem
}{
	{name: "public", load: unseenDesignSystem},
	{name: "ahra", load: loadWave1DesignSystem},
}

// forEachTailwindSystem runs Tailwind assertion once per design system, each as its own subtest, so a
// machine without the ahra corpus skips that subtest by name and still runs the public one.
func forEachTailwindSystem(test *testing.T, run func(test *testing.T, system *LoadedDesignSystem)) {
	test.Helper()
	for _, tailwindSystem := range tailwindSystems {
		test.Run(tailwindSystem.name, func(test *testing.T) {
			test.Parallel()
			run(test, tailwindSystem.load(test))
		})
	}
}
