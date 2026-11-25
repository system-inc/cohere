package us10

import (
	"os"
)

func databaseAddress() string {
	database_url := os.Getenv("DATABASE_URL")
	return database_url
}
