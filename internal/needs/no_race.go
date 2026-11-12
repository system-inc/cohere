//go:build !race

package needs

// raceDetector is whether this binary was built with -race.
const raceDetector = false
