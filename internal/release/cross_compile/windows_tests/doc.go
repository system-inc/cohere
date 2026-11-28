// Package windows_tests holds one check, that every package of the module type-checks for Windows with its
// tests, and the check reads every directory of the module to prove it, on purpose: a test anywhere that
// cannot build for Windows is what it looks for. It lives apart from cross_compile because Go's test cache
// keys a package's result on every file and directory its tests read: here, only this package reruns when
// any file of the module changes, and cross_compile, which reads only the program it judges, stays cached
// (#0zdyk51). Its known-dirty control, TestTypeCheckOfTestsCatchesAUnixOnlyTestHelper, reads only a fixture,
// so it stays in cross_compile beside cross_compile.TypeCheck.
package windows_tests
