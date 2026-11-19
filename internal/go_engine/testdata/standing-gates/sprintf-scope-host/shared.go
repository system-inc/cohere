package gate

import "fmt"

type name []byte

func target(value name) {
	fmt.Println(fmt.Sprintf("%s", value))
}
