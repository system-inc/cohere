package timing

import "time"

// cost is one row's CPU and how many times it was added to.
type cost struct {
	cpu   time.Duration
	count int64
}

// costs is a row for each name, as it is added to.
type costs map[string]*cost

// add adds cpu and count to the row named name, making the row on its first add.
func (rows costs) add(name string, cpu time.Duration, count int64) {
	row, known := rows[name]
	if !known {
		row = &cost{}
		rows[name] = row
	}
	row.cpu += cpu
	row.count += count
}
