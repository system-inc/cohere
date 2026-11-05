package main

import (
	"github.com/system-inc/cohere/internal/gitignore"
)

// gitignoreScope is internal/gitignore's matcher for one directory (#ndtgy1w), as discovery asks it.
type gitignoreScope struct {
	matcher *gitignore.Matcher
}

func newGitignoreScope(repositoryRoot string) (ignoreScope, error) {
	matcher, newGitignoreError := gitignore.New(repositoryRoot)
	if newGitignoreError != nil {
		return nil, newGitignoreError
	}
	return gitignoreScope{matcher: matcher}, nil
}

func (scope gitignoreScope) Enter(relativeDirectory string) (ignoreScope, error) {
	entered, enterError := scope.matcher.Enter(relativeDirectory)
	if enterError != nil {
		return nil, enterError
	}
	return gitignoreScope{matcher: entered}, nil
}

func (scope gitignoreScope) Ignored(relativePath string, isDirectory bool) bool {
	ignored, _ := scope.matcher.Ignored(relativePath, isDirectory)
	return ignored
}
