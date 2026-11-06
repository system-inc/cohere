package us03

type Order_Line struct {
	unit_price int
}

func price(line Order_Line) int {
	return line.unit_price
}
