package check

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// sessionChildVariable, set to a sessionChildRequest as JSON, makes the test binary run that one session and print a
// sessionChildReply instead of running the tests. A session in a process of its own is how a test gets gob's type
// numbers to move: gob numbers each type the first time a process encodes one and keeps the number for the
// process's life, so within one process a fact's bytes never move, while across two that encoded other types first
// they do, as two runs of the engine do.
const sessionChildVariable = "COHERE_CHECK_SESSION_CHILD"

// sessionChildRequest is one session another process runs: callsAnalyzer's rule over the module at Root, with the
// cache in Store, after encoding Preamble types gob has not seen.
type sessionChildRequest struct {
	Root     string
	Store    string
	Preamble int
}

// sessionChildReply is what the session did: the cache's counts, the output digest of each package it saved by ID,
// whether its Result is a run's without the cache over the same tree, and markFact's bytes as that process encodes
// them, which say gob's number for it there.
type sessionChildReply struct {
	Cache       CacheCounts
	Outputs     map[string]string
	Agrees      bool
	MarkEncoded string
}

// gobPreambleFirst, gobPreambleSecond and gobPreambleThird are types a session's process encodes before anything
// else when its request asks, each taking the next of gob's numbers, so every type the session encodes after them
// is numbered later than in a process that did not.
type (
	gobPreambleFirst  struct{ First int }
	gobPreambleSecond struct{ Second int }
	gobPreambleThird  struct{ Third int }
)

// runSessionChild runs the session request asks for, as JSON, and prints its reply as JSON.
func runSessionChild(request string) error {
	var asked sessionChildRequest
	if decodeError := json.Unmarshal([]byte(request), &asked); decodeError != nil {
		return fmt.Errorf("reading the request: %w", decodeError)
	}
	encoder := gob.NewEncoder(io.Discard)
	for _, preamble := range []any{gobPreambleFirst{1}, gobPreambleSecond{2}, gobPreambleThird{3}}[:asked.Preamble] {
		if encodeError := encoder.Encode(preamble); encodeError != nil {
			return fmt.Errorf("encoding the preamble: %w", encodeError)
		}
	}
	rules := []rule.Rule{houseRule(callsAnalyzer)}
	session := NewCacheSession(cachingAt(asked.Store, rules), rules)
	loaded, loadError := load.PackagesCached(asked.Root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session})
	if loadError != nil {
		return fmt.Errorf("loading %s: %w", asked.Root, loadError)
	}
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld})
	if runError != nil {
		return fmt.Errorf("running with the cache: %w", runError)
	}
	uncachedLoad, uncachedLoadError := load.Packages(asked.Root, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if uncachedLoadError != nil {
		return fmt.Errorf("loading %s without the cache: %w", asked.Root, uncachedLoadError)
	}
	uncached, uncachedError := Run(uncachedLoad.Packages, rules, Options{})
	if uncachedError != nil {
		return fmt.Errorf("running without the cache: %w", uncachedError)
	}
	marked, markError := encodeFact(new(markFact))
	if markError != nil {
		return fmt.Errorf("encoding a markFact: %w", markError)
	}
	reply := sessionChildReply{
		Cache:       result.Cache,
		Outputs:     map[string]string{},
		Agrees:      reflect.DeepEqual(withoutTimesOrCache(result), withoutTimesOrCache(uncached)),
		MarkEncoded: base64.StdEncoding.EncodeToString(marked.Encoded),
	}
	for saved, output := range session.outputs {
		reply.Outputs[saved.ID] = hex.EncodeToString(output[:])
	}
	encoded, encodeError := json.Marshal(reply)
	if encodeError != nil {
		return fmt.Errorf("writing the reply: %w", encodeError)
	}
	_, writeError := os.Stdout.Write(encoded)
	return writeError
}

// sessionInAnotherProcess runs callsAnalyzer's rule over root with the cache in store, in another process of the test
// binary that encodes preamble types first (see sessionChildVariable), and returns what that session did.
func sessionInAnotherProcess(test *testing.T, root string, store string, preamble int) sessionChildReply {
	test.Helper()
	request, requestError := json.Marshal(sessionChildRequest{Root: root, Store: store, Preamble: preamble})
	if requestError != nil {
		test.Fatalf("writing the request: %v", requestError)
	}
	command := exec.Command(os.Args[0], "-test.run=^$")
	command.Env = append(os.Environ(), sessionChildVariable+"="+string(request))
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		test.Fatalf("running the session in another process: %v\n%s", runError, standardError.String())
	}
	var reply sessionChildReply
	if decodeError := json.Unmarshal(output, &reply); decodeError != nil {
		test.Fatalf("reading the other process's reply %q: %v", output, decodeError)
	}
	return reply
}
