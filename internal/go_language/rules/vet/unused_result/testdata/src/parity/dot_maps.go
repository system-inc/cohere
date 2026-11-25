package parity

import . "maps"

func dotImportedMaps(table map[string]int) {
	Keys(table)        // reported
	Clone(table)       // reported, as maps.Clone
	Copy(table, table) // clean
}
