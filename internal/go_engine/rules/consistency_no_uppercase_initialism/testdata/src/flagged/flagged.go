package flagged

import (
	"encoding/json"

	"elsewhere"
)

const ID = 1 // want `"ID" spells an initialism in capitals. Use "Id"`

var userID = 2 // want `"userID" spells an initialism in capitals. Use "userId"`

var HTTPClient = "client" // want `"HTTPClient" .* Use "HttpClient"`

type JSONEncoder struct{} // want `"JSONEncoder" .* Use "JsonEncoder"`

type HTTPServer struct { // want `"HTTPServer" .* Use "HttpServer"`
	// The tag carries the external spelling, so the field's own name is ours.
	UserID   string   `json:"userID"` // want `"UserID" .* Use "UserId"`
	homeURLs []string // want `"homeURLs" .* Use "homeUrls"`
}

func ParseURL(requestID string) (responseURL string) { // want `"ParseURL" .* Use "ParseUrl"` `"requestID" .* Use "requestId"` `"responseURL" .* Use "responseUrl"`
	parseURLPath := requestID // want `"parseURLPath" .* Use "parseUrlPath"`
	responseURL = parseURLPath
	return responseURL
}

func Digest(names []string) int {
	SHA256Sum := 0              // want `"SHA256Sum" .* Use "Sha256Sum"`
	for nameID := range names { // want `"nameID" .* Use "nameId"`
		SHA256Sum += nameID
	}
	var URLsToFetch []string // want `"URLsToFetch" .* Use "UrlsToFetch"`
	_ = URLsToFetch
	return SHA256Sum
}

func Retry() {
retryHTTP: // want `"retryHTTP" .* Use "retryHttp"`
	for {
		break retryHTTP
	}
}

func Describe(subject any) string {
	switch valueID := subject.(type) { // want `"valueID" .* Use "valueId"`
	case string:
		return valueID
	}
	return ""
}

func Lookup[KeyID comparable](keys map[KeyID]string) int { // want `"KeyID" .* Use "KeyId"`
	return len(keys)
}

// A receiver's name and a method's name are both ours when no interface fixes them.
func (serverAPI *HTTPServer) ServeJSON() {} // want `"serverAPI" .* Use "serverApi"` `"ServeJSON" .* Use "ServeJson"`

// fetcher is our own interface, so its method and the method implementing it are ours and renamed together.
type fetcher interface {
	FetchURL() string // want `"FetchURL" .* Use "FetchUrl"`
}

type page struct{}

func (current page) FetchURL() string { return "" } // want `"FetchURL" .* Use "FetchUrl"`

var _ fetcher = page{}

// record spells MarshalJSON with a signature json.Marshaler does not have, so no interface fixes the name.
type record struct{}

func (current record) MarshalJSON() string { return "" } // want `"MarshalJSON" .* Use "MarshalJson"`

var _ = json.Valid

// partial has only one of elsewhere.Located's two methods, so it does not implement it and the name is ours.
type partial struct{}

func (current partial) HomeURL() string { return "" } // want `"HomeURL" .* Use "HomeUrl"`

// keyed shares a name with elsewhere.Keyed's method and takes a parameter it does not, so no instantiation fixes it.
type keyed struct{}

func (current keyed) KeyID(extra int) string { return "" } // want `"KeyID" .* Use "KeyId"`

var _ elsewhere.Identified = nil

// A plural initialism ends in a lone s.
var userIDs []int // want `"userIDs" .* Use "userIds"`

var MD5 = "digest" // want `"MD5" .* Use "Md5"`

// A sentence in a comment never names anything, so URL here is quiet; the declaration below is not.
var sessionTTL = 3 // want `"sessionTTL" .* Use "sessionTtl"`
