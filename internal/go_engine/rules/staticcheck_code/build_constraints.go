package staticcheck_code

import (
	"go/ast"
	"go/build/constraint"
	"path/filepath"
	"strings"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// BuildConstraints is the build constraint file is compiled under, and whether it has one: its `//go:build` line,
// joined by && with the operating system and architecture its name carries, such as the linux and amd64 of
// name_linux_amd64.go. It is staticcheck's code.BuildConstraints, from honnef.co/go/tools@v0.8.1
// analysis/code/code.go, reading the known operating systems and architectures from staticcheck's knowledge package,
// as data.
//
// As staticcheck's does, it reads the first `//go:build` line of each comment group before the package clause, the
// last group's winning, so a line that does not parse leaves no constraint from the comments when it is the last read.
// An old `// +build` line is no constraint here.
func BuildConstraints(context *rule.Context, file *ast.File) (constraint.Expr, bool) {
	var expression constraint.Expr
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if comment.Pos() > file.Package {
				break
			}
			if !strings.HasPrefix(comment.Text, "//go:build") {
				continue
			}
			parsed, parseError := constraint.Parse(comment.Text)
			if parseError != nil {
				parsed = nil
			}
			expression = parsed
			break
		}
	}
	// The name as written, never as a `//line` directive renames it, since the go tool reads the file's own name.
	named := constraintsFromName(context.FileSet.PositionFor(file.Pos(), false).Filename)
	if named != nil {
		if expression == nil {
			expression = named
		} else {
			expression = &constraint.AndExpr{X: expression, Y: named}
		}
	}
	return expression, expression != nil
}

// constraintsFromName is the constraint a file's name puts on it: the known operating system or architecture after
// its last underscore, or both after its last two, a `_test` suffix dropped first, and nil when it names neither. A
// name such as name_windows_unknown.go names nothing, since its last element is no known architecture, and windows
// then counts for nothing either.
func constraintsFromName(path string) constraint.Expr {
	name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".go"), "_test")
	var operatingSystem string
	var architecture string
	switch strings.Count(name, "_") {
	case 0:
	case 1:
		_, last, _ := strings.Cut(name, "_")
		if _, isOperatingSystem := knowledge.KnownGOOS[last]; isOperatingSystem {
			operatingSystem = last
		} else if _, isArchitecture := knowledge.KnownGOARCH[last]; isArchitecture {
			architecture = last
		}
	default:
		lastUnderscore := strings.LastIndex(name, "_")
		last := name[lastUnderscore+1:]
		if _, isOperatingSystem := knowledge.KnownGOOS[last]; isOperatingSystem {
			operatingSystem = last
		} else if _, isArchitecture := knowledge.KnownGOARCH[last]; isArchitecture {
			architecture = last
			// staticcheck reads as the operating system all that lies between the first underscore and the last, not
			// the element before the last, so a_b_linux_amd64.go names amd64 alone.
			_, afterFirst, _ := strings.Cut(name[:lastUnderscore], "_")
			if _, isOperatingSystem := knowledge.KnownGOOS[afterFirst]; isOperatingSystem {
				operatingSystem = afterFirst
			}
		}
	}
	var expression constraint.Expr
	if operatingSystem != "" {
		expression = &constraint.TagExpr{Tag: operatingSystem}
	}
	if architecture != "" {
		if expression == nil {
			expression = &constraint.TagExpr{Tag: architecture}
		} else {
			expression = &constraint.AndExpr{X: expression, Y: &constraint.TagExpr{Tag: architecture}}
		}
	}
	return expression
}
