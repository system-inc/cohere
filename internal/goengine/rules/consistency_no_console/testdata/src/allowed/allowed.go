package allowed

import (
	"fmt"
	"io"
	"log"
	"os"
)

// A writer handed in is the caller's choice of where the text goes.
func render(writer io.Writer, total int) error {
	_, writeError := fmt.Fprintf(writer, "%d done\n", total)
	return writeError
}

// Formatting to a string or an error writes nothing.
func describe(total int) (string, error) {
	return fmt.Sprintf("%d done", total), fmt.Errorf("stopping after %d", total)
}

// A *log.Logger's methods write to the writer it was built with, the standard logger's included.
func logThrough(logger *log.Logger, reason string) {
	logger.Println(reason)
	logger.Printf("%s", reason)
	log.New(os.Stderr, "allowed: ", 0).Print(reason)
	log.Default().Println(reason)
	printLine := (*log.Logger).Println
	printLine(logger, reason)
}

// Configuring the standard logger writes nothing.
func configure(writer io.Writer) {
	log.SetOutput(writer)
	log.SetFlags(0)
	log.SetPrefix("allowed: ")
}
