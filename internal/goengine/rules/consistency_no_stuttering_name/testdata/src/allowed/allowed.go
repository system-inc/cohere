package allowed

import (
	"encoding/json"
	"net/http"
)

type outcome struct {
	Status int
	Value  string
}

// A generic name doing its job: response is exactly what it is, and it reaches a member with a word of its own.
func fetched(client *http.Client, request *http.Request) (int, error) {
	response, requestError := client.Do(request)
	if requestError != nil {
		return 0, requestError
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

// Two generic words, but different ones, so the name is not borrowing its member's.
func differentWords(result outcome) string {
	return result.Value
}

// A name that says which outcome this is reaches the same member with no stutter.
func namedForWhatItHolds(parsedOutcome outcome, data []byte) (int, error) {
	decodeError := json.Unmarshal(data, &parsedOutcome)
	return parsedOutcome.Status, decodeError
}

// Renamed for what it holds, the repair the message asks for.
func repaired(entry outcome) string {
	return entry.Value
}
