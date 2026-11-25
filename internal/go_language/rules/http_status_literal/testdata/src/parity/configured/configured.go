// Package configured is judged under its own staticcheck.conf, which leaves 301 and 418 alone in place of the
// default list, so 404 and 200 are reported here and 301 and 418 are not.
package configured

import "net/http"

func statuses() {
	_ = http.StatusText(404)
	_ = http.StatusText(301)
	_ = http.StatusText(418)
	_ = http.StatusText(200)
	_ = http.StatusText(503)
}
