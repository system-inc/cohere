// Package differences holds the shapes where the port could most plausibly part from S1031's original, and records
// none, since the two agree on each.
package differences

type list []int

type lists = list

// constraint names a slice type set through a named interface with a method, judged by its one term.
type constraint interface {
	~[]string
	Len() int
}

type strings []string

func (values strings) Len() int { return len(values) }

// labelsAndBlocks nests labels and blocks of one statement around the range, each looked through.
func labelsAndBlocks(values list) {
	if values != nil {
	first:
		{
		second:
			{
			third:
				for range values {
					if len(values) > 3 {
						goto first
					}
					if len(values) > 2 {
						goto second
					}
					continue third
				}
			}
		}
	}
	// A labeled block of two statements holds no range alone.
	if values != nil {
	fourth:
		{
			for range values {
				goto fourth
			}
			_ = values
		}
	}
}

// aliases reach the slice through an alias of a named slice.
func aliases(values lists) {
	if values != nil {
		for range values {
		}
	}
}

// constrained ranges over a type parameter whose constraint has a method beside its one slice term.
func constrained[T constraint](values T) {
	if values != nil {
		for range values {
		}
	}
}

// chains of else ifs: each inner if is judged alone, and an outer one with an else if is not the query's.
func chains(values []int, counts map[string]int, flag bool) {
	if flag {
	} else if counts != nil {
		for range counts {
		}
	} else if values != nil {
		for range values {
		}
	}
	if flag {
	} else if values != nil {
		for range values {
		}
	} else {
	}
}

// closures check a variable they capture, and one shadowed in the closure's own scope.
func closures(values []int) func() {
	return func() {
		if values != nil {
			for range values {
			}
		}
		values := map[int]int{}
		if values != nil {
			for range values {
			}
		}
	}
}

// nestedIfs are an if whose range holds a finding of its own.
func nestedIfs(matrix [][]int) {
	if matrix != nil {
		for _, row := range matrix {
			if row != nil {
				for range row {
				}
			}
		}
	}
}
