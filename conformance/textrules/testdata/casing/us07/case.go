package us07

import (
	"os"
)

func databaseAddress() string {
	DATABASE_URL := os.Getenv("DATABASE_URL")
	return DATABASE_URL
}
