package dotted

import (
	"fmt"
	. "log"
)

// A dot import names log's functions by identifiers alone, and the message names them as log's.
func dotLog(logger *Logger) {
	Print(fmt.Sprintf("%d", 1))        // reported
	Fatalln(fmt.Sprintf("%d", 1))      // reported
	Printf(fmt.Sprintf("%d", 1))       // left alone
	logger.Print(fmt.Sprintf("%d", 1)) // reported
}
