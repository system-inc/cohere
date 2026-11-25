package load

import "sync"

// answerHooks are, by workspace, what a test has called with each answer go env gives there
// (HookGoEnvironmentAnswers), under lock. Empty but in a test.
type answerHooks struct {
	lock        sync.Mutex
	byWorkspace map[string]func(environment []string, answer map[string]string)
}

// goEnvironmentHooks are the run's answerHooks.
var goEnvironmentHooks = answerHooks{byWorkspace: map[string]func(environment []string, answer map[string]string){}}

// HookGoEnvironmentAnswers calls hook with each answer go env gives in workspace, absolute, and the environment the
// caller asked under, before anything reads the answer and on the goroutine that asked, until stop is called: a test
// changes an answer there, as another toolchain's would read, or holds it back. Only a test calls it; it is exported
// for cohere-go's tests, as WatchGoEnvironmentAsks is.
func HookGoEnvironmentAnswers(
	workspace string,
	hook func(environment []string, answer map[string]string),
) (stop func()) {
	goEnvironmentHooks.lock.Lock()
	defer goEnvironmentHooks.lock.Unlock()
	goEnvironmentHooks.byWorkspace[workspace] = hook
	return func() {
		goEnvironmentHooks.lock.Lock()
		defer goEnvironmentHooks.lock.Unlock()
		delete(goEnvironmentHooks.byWorkspace, workspace)
	}
}

// called calls the hook a test set for workspace, if any, with environment and answer.
func (hooks *answerHooks) called(workspace string, environment []string, answer map[string]string) {
	hooks.lock.Lock()
	hook := hooks.byWorkspace[workspace]
	hooks.lock.Unlock()
	if hook != nil {
		hook(environment, answer)
	}
}
