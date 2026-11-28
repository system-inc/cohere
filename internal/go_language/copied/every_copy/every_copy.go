// Package every_copy holds every copy in the module to the upstream text recorded for it, in its one test.
//
// The test walks the module for every Go file that declares itself a copy (internal/go_language/copied) and holds
// each to its recorded text through internal/go_language/copied/upstream_text. Finding every copy is the point, so
// it reads every Go file in the module, and Go's test cache keys it on all of them: it reruns on any change. It lives
// in a package of its own, with nothing else in it, so that is the only test that does; copied's and upstream_text's
// own tests read only their own files and replay when nothing they judge changed (#pdvcww2).
package every_copy
