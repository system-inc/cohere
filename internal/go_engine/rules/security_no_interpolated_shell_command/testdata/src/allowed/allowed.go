// Package allowed runs shells and programs the way the rule asks for, and splices in only text that holds no shell
// syntax. Nothing here is ever run.
package allowed

import (
	"fmt"
	"os/exec"
	"strconv"
)

// workspace is a named constant, text the author wrote.
const workspace = "/tmp/workspace"

// Count is a named integer type with no method that prints it as anything but digits.
type Count int

// The value goes after the script as its own argument, which the shell reads as $1 and never parses.
func valueAsTheShellsArgument(directory string) {
	_ = exec.Command("sh", "-c", `cd "$1" && make`, "sh", directory)
	_ = exec.Command("bash", "-c", "ls \"$1\" | wc -l", "bash", directory)
	_ = exec.Command("sh", "-c", fmt.Sprintf("ls %d", 3), "sh", directory)
}

// With no shell, every argument reaches the program as it is.
func noShell(directory string, author string) {
	command := exec.Command("make")
	command.Dir = directory
	_ = exec.Command("ls", directory)
	_ = exec.Command("git", "log", fmt.Sprintf("--author=%s", author))
	_ = exec.Command("git", "-C", directory, "log")
}

// A number or a boolean prints digits, a sign, a point and letters, none of them shell syntax.
func numbersAndBooleans(pid int, seconds float64, verbose bool, count Count, size uint64) {
	_ = exec.Command("sh", "-c", fmt.Sprintf("kill -TERM %d", pid))
	_ = exec.Command("sh", "-c", fmt.Sprintf("sleep %f", seconds))
	_ = exec.Command("sh", "-c", fmt.Sprintf("run --verbose=%t", verbose))
	_ = exec.Command("sh", "-c", fmt.Sprintf("head -n %d", count))
	_ = exec.Command("sh", "-c", "kill "+strconv.Itoa(pid))
	_ = exec.Command("sh", "-c", "truncate -s "+strconv.FormatUint(size, 10)+" file")
	_ = exec.Command("sh", "-c", "sleep "+strconv.FormatFloat(seconds, 'f', -1, 64))
	_ = exec.Command("sh", "-c", "run --verbose="+strconv.FormatBool(verbose))
	_ = exec.Command("sh", "-c", "kill "+strconv.FormatInt(int64(pid), 10))
}

// Constants are text the author wrote, however they are joined.
func constants() {
	const name = "build"
	_ = exec.Command("sh", "-c", "ls "+workspace)
	_ = exec.Command("sh", "-c", "ls "+workspace+"/"+name)
	_ = exec.Command("sh", "-c", fmt.Sprintf("ls %s/%s", workspace, name))
	_ = exec.Command("sh", "-c", fmt.Sprintf("echo done"))
	_ = exec.Command("sh", "-c", "ps -Ao pid=,command= | grep worker")
	script := fmt.Sprintf("ls %s", workspace)
	_ = exec.Command("sh", "-c", script)
}

// A quoted heredoc's body is data the shell expands nothing in, which is how SQL is fed to a client. The rule declines
// any script with << in it rather than judge values the shell may never parse.
func quotedHeredoc(query string) {
	_ = exec.Command("sh", "-c", fmt.Sprintf("cat <<'EOSQL' | sqlite3 data.db\n%s\nEOSQL", query))
	_ = exec.Command("sh", "-c", "cat <<'EOSQL' | sqlite3 data.db\n"+query+"\nEOSQL")
}
