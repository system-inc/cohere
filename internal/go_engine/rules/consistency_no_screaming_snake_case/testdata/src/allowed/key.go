package allowed

import "os"

// A name read from an external key spelled exactly as the name is mirrors that key, on both sides of the boundary.
var SERVICE_URL = os.Getenv("SERVICE_URL")

const tokenKey = "API_TOKEN"

func environment() (string, string, string) {
	DATABASE_URL := os.Getenv("DATABASE_URL")
	// The key is the argument's constant value, so a named constant spelled as the name counts.
	API_TOKEN := os.Getenv(tokenKey)
	// One call gives both names their values, and the key is spelled as the first.
	API_HOST, found := os.LookupEnv("API_HOST")
	if !found {
		return DATABASE_URL, API_TOKEN, SERVICE_URL
	}
	return DATABASE_URL, API_TOKEN, API_HOST
}
