package lb01

func findTotal(grid [][]int) int {
	found := 0
outer:
	for _, row := range grid {
		for _, cell := range row {
			if cell < 0 {
				continue outer
			}
			if cell > 9 {
				break outer
			}
			found += cell
		}
	}
	return found
}
