// Package inherit greets on every platform, and on windows names it. Each of its files holds a planted literal, which
// a syntax-only probe reports, so a build beside the host's inherits the host's findings on the files both compile.
package inherit

// Greeting is what every build greets with.
const Greeting = "planted greeting"
