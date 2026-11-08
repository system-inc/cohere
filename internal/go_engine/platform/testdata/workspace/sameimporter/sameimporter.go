// Package sameimporter imports sameseed, whose export is the same in both builds.
package sameimporter

import "example.com/workspace/sameseed"

// Twice is sameseed's value twice.
func Twice() int { return sameseed.Value() * 2 }
