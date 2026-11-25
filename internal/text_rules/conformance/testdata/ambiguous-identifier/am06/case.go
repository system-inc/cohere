package am06

import (
	"os"
)

func safely() error {
	if e := os.Remove("x"); e != nil {
		return e
	}
	return nil
}
