// Package facts holds a fact note of each shape staticcheck's fixtures write, beside diag notes.
package facts

func plain(values []int) {} //@ fact(values, "needs even elements")

func pattern(values []int) {} //@ fact(values, re`needs \w+ elements`)

func two(first []int, second []int) {} //@ fact(first, "one"), fact(second, "two")

func mixed(values []int) { //@ fact(values, "beside"), diag(`a diag`)
	/*@ fact(
		values,
		`over lines`,
	) */
	_ = values
}
