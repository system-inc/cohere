package corpus

import (
	"os"
	"os/exec"
	"testing"
)

// runProbe runs TestRootSkipsWhenUnsetAndFailsWhenWrong again in a child process with the Ahra corpus's
// variable set to value, and returns its verbose output.
func runProbe(t *testing.T, value string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run", "^TestRootSkipsWhenUnsetAndFailsWhenWrong$", "-test.v")
	command.Env = append(os.Environ(), "COHERE_CORPUS_PROBE=1", Ahra.Variable+"="+value)
	output, _ := command.CombinedOutput()
	return string(output)
}
