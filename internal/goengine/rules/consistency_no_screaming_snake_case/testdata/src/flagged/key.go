package flagged

import "os"

const addressKey = "DATABASE_ADDRESS"

func environment() (string, string, string, string) {
	// A key spelled any other way mirrors nothing, written out or through a constant.
	DATABASE_URL := os.Getenv("DATABASE_ADDRESS") // want "`DATABASE_URL` is .*; spell it `databaseUrl`"
	DATABASE_HOST := os.Getenv(addressKey)        // want "`DATABASE_HOST` is in screaming snake case"
	// A conversion reads nothing from outside.
	API_TOKEN := string("API_TOKEN") // want "`API_TOKEN` is in screaming snake case; spell it `apiToken`"
	// Only the declaration's own initializer counts, not a key read into another name first.
	read := os.Getenv("SERVICE_URL")
	SERVICE_URL := read // want "`SERVICE_URL` is in screaming snake case; spell it `serviceUrl`"
	return DATABASE_URL, DATABASE_HOST, API_TOKEN, SERVICE_URL
}
