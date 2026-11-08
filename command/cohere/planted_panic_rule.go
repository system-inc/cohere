//go:build cohere_planted_panic

package main

import (
	"strings"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// plantedPanicMarker is the text that makes the planted rule panic on a file.
const plantedPanicMarker = "cohere-test: panic here"

// A rule that panics on any file holding plantedPanicMarker, so a test can watch a whole run survive a
// rule's crash and say so. Compiled only under the cohere_planted_panic tag, which only the binary
// TestARuleCrashFailsTheRunAndNamesItselfAsCohere builds carries: no shipped binary has it.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: rule.Rule[rule.NoOptions]{
		Name: "cohere-test/panics-on-marked-file",
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			if strings.Contains(ruleContext.SourceFile.Text(), plantedPanicMarker) {
				//cohere:ignore correctness-require-invariant-reason this crash is the test's plant, meant to happen
				panic("planted: Unhandled case in Node.Text: *ast.ComputedPropertyName")
			}
			return nil
		},
	}})
}
