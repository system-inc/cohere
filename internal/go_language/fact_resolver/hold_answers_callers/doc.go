// Package hold_answers_callers holds one guard, that no file of the module but a test's calls
// fact_resolver.Resolver.HoldAnswers, and the guard reads every Go file of the module to prove it. It lives apart from
// fact_resolver because Go's test cache keys a package's result on every file its tests read: here, only this package
// reruns when any file changes, and fact_resolver's own tests stay cached (#r1b5av9).
package hold_answers_callers
