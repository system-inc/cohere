// Package staticcheck_facts holds a fixture in staticcheck's form that expects a fact: one about First, and none about
// Second.
package staticcheck_facts

func First() {} //@ fact(First, "marked")

func Second() {}
