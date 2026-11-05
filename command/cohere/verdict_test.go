//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The verdict descriptor the dispatcher hands the engine is the engine's alone. A process it starts, the
// Swift engine or any other, neither holds the descriptor nor finds the variable naming it, since a
// grandchild writing a byte to "descriptor 3" would write it into whatever 3 is there (#zqsdzbq).
//
// The descriptor arrives the way the dispatcher passes it, without close-on-exec, so a child would hold
// it: the first spawn proves that, which is what makes the second one's answer mean something.
// Not parallel: it sets the verdict variable with test.Setenv, which a parallel test may not
func TestTheVerdictDescriptorIsNotInheritedByAChild(test *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		test.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	descriptor, err := syscall.Dup(int(writer.Fd()))
	if err != nil {
		test.Fatal(err)
	}
	probe := func() string {
		script := fmt.Sprintf("if [ -e /dev/fd/%d ]; then echo open; else echo closed; fi; echo ${%s:-unset}", descriptor, VerdictVariable)
		output, outputError := exec.Command("/bin/sh", "-c", script).Output()
		if outputError != nil {
			test.Fatal(outputError)
		}
		return strings.Join(strings.Fields(string(output)), " ")
	}

	test.Setenv(VerdictVariable, strconv.Itoa(descriptor))
	if before := probe(); !strings.HasPrefix(before, "open") {
		test.Fatalf("a child did not see the descriptor even before it was taken (%s), so this proves nothing", before)
	}
	file := takeVerdictFile()
	if file == nil {
		test.Fatal("the verdict descriptor was not taken")
	}
	defer file.Close()
	if after := probe(); after != "closed unset" {
		test.Errorf("a child started after the verdict descriptor was taken sees %q, want the descriptor closed and the variable unset", after)
	}
}

// A verdict variable naming a descriptor that is not a pipe is not the dispatcher's, and nothing is written
// to it. A script that opened a log on 3 (`3>log`) and inherited the variable from somewhere would
// otherwise find a stray byte in its log. A pipe in the same place is taken, which is what makes the
// refusal mean something.
// Not parallel: it sets the verdict variable with test.Setenv and assigns the package-level verdictFile
func TestAVerdictDescriptorThatIsNotAPipeIsLeftAlone(test *testing.T) {
	logPath := test.TempDir() + "/log"
	log, err := os.Create(logPath)
	if err != nil {
		test.Fatal(err)
	}
	defer log.Close()
	test.Setenv(VerdictVariable, strconv.Itoa(int(log.Fd())))
	verdictFile = takeVerdictFile()
	if verdictFile != nil {
		test.Fatal("a regular file was taken as the verdict descriptor")
	}
	if sendVerdict(1) {
		test.Fatal("a verdict was sent with no pipe to send it to")
	}
	if _, writeError := log.Write([]byte("still open\n")); writeError != nil {
		test.Fatalf("the regular file was closed: %v", writeError)
	}
	if contents, _ := os.ReadFile(logPath); string(contents) != "still open\n" {
		test.Errorf("the regular file holds %q, want only what its owner wrote", contents)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		test.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	// A descriptor of its own for takeVerdictFile to own and close. Handed the writer's, it would make a
	// second *os.File around one descriptor, whose finalizer closes that number again after the writer
	// has, by then most likely another test's pipe: a command's output read end closed out from under the
	// poller hangs its Wait forever (#zqsdzbq, reproduced at round 145 of a stress loop).
	descriptor, err := syscall.Dup(int(writer.Fd()))
	if err != nil {
		test.Fatal(err)
	}
	test.Setenv(VerdictVariable, strconv.Itoa(descriptor))
	file := takeVerdictFile()
	if file == nil {
		syscall.Close(descriptor)
		test.Fatal("a pipe was not taken as the verdict descriptor")
	}
	file.Close()
}
