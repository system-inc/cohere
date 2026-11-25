package am02

func total(values []string) int {
	count := 0
	for _, value := range values {
		count += len(value)
	}
	return count
}
