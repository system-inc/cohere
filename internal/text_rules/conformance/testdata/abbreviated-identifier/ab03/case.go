package ab03

import (
	"strings"
)

func joinAll(args ...string) string {
	return strings.Join(args, ",")
}
