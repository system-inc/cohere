// Package faking writes *testing.T of a package it imports under the name testing, which is not the standard
// library's, so testinggoroutine, which judges only a package importing testing, never hears its go statements.
package faking

import testing "mocktest"

// Start takes a T of the imported package's and starts helper with go.
func Start(t *testing.T) { go helper(t) }

// helper logs.
func helper(t *testing.T) { t.Log("helper") }
