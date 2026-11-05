// Package concurrency_no_kill_by_name is the Go engine's rule against stopping processes by name.
//
// pkill, killall, and a kill given a name rather than a pid stop every process on the machine whose name matches, not
// the one the code started. On a machine shared by several agents and runs, that takes their work down with it, and
// the failure lands on whoever was running, far from the line that caused it. The code that started a process holds
// its pid, so it stops that process: process.Kill(), or syscall.Kill(pid, signal).
package concurrency_no_kill_by_name

import (
	"go/ast"
	"go/constant"
	"go/types"
	"path"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/concurrency-no-kill-by-name"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "concurrency_no_kill_by_name",
	Doc: "Flags exec.Command and exec.CommandContext running pkill or killall, kill with an operand that is not a " +
		"pid, or sh, bash or zsh -c with a script that runs pkill or killall, since stopping by name takes every " +
		"matching process on the machine with it. Only constant programs and arguments are judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var killByNameMessage = policy.MessageOf(Name, "killByName")

// byNamePrograms stop processes by name whatever their arguments say, so the program alone decides.
var byNamePrograms = map[string]bool{"pkill": true, "killall": true}

// shellPrograms run the script after -c, which is where a pkill or killall can hide.
var shellPrograms = map[string]bool{"sh": true, "bash": true, "zsh": true}

// argument is one argument of a command, with its text when it is a constant string.
type argument struct {
	text       string
	isConstant bool
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			programIndex, isCommand := programIndexOf(pass, call)
			if !isCommand || len(call.Args) <= programIndex {
				return true
			}
			program, isConstant := constantString(pass, call.Args[programIndex])
			if !isConstant {
				return true
			}
			// A path names the same program, so /usr/bin/pkill is judged as pkill.
			if killer, killsByName := judge(path.Base(program), argumentsOf(pass, call, programIndex)); killsByName {
				report(pass, call.Args[programIndex], killer)
			}
			return true
		})
	}
	return nil, nil
}

// programIndexOf is where the program sits among a call's arguments when the call is os/exec's Command or
// CommandContext, resolved by the callee's object so a function of the same name in another package is never one.
func programIndexOf(pass *analysis.Pass, call *ast.CallExpr) (int, bool) {
	function, isFunc := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunc || function.Pkg() == nil || function.Pkg().Path() != "os/exec" || function.Signature().Recv() != nil {
		return 0, false
	}
	switch function.Name() {
	case "Command":
		return 0, true
	case "CommandContext":
		// The context comes first.
		return 1, true
	}
	return 0, false
}

// argumentsOf is what a command is given after its program. A spread is read when it is a slice literal written in
// place; any other spread is a slice built elsewhere, so its arguments are unknown and the result is nil.
func argumentsOf(pass *analysis.Pass, call *ast.CallExpr, programIndex int) []argument {
	expressions := call.Args[programIndex+1:]
	if call.Ellipsis.IsValid() {
		literal, isLiteral := ast.Unparen(expressions[len(expressions)-1]).(*ast.CompositeLit)
		if !isLiteral {
			return nil
		}
		for _, element := range literal.Elts {
			// An indexed element, []string{2: "x"}, places its value somewhere other than where it is written.
			if _, isKeyed := element.(*ast.KeyValueExpr); isKeyed {
				return nil
			}
		}
		expressions = literal.Elts
	}
	arguments := make([]argument, 0, len(expressions))
	for _, expression := range expressions {
		text, isConstant := constantString(pass, expression)
		arguments = append(arguments, argument{text: text, isConstant: isConstant})
	}
	return arguments
}

// constantString is expression's value when the type checker knows it as a constant string.
func constantString(pass *analysis.Pass, expression ast.Expr) (string, bool) {
	value := pass.TypesInfo.Types[expression].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}

// judge is whether running program with arguments stops processes by name, and the program that does it: program
// itself, or for a shell, the pkill or killall its script runs.
func judge(program string, arguments []argument) (string, bool) {
	switch {
	case byNamePrograms[program]:
		return program, true
	case program == "kill":
		return program, killNamesAProcess(arguments)
	case shellPrograms[program]:
		script, hasScript := shellScript(arguments)
		if !hasScript {
			return "", false
		}
		return scriptKiller(script)
	}
	return "", false
}

// killNamesAProcess is whether kill's arguments hold an operand that is a name, a constant that doesn't parse as an
// integer. util-linux's kill takes a name there and stops every process carrying it. Every option, a signal like -9 or
// -TERM included, opens with a dash, and the value an option takes is no operand, so neither is judged.
func killNamesAProcess(arguments []argument) bool {
	for _, given := range arguments {
		// Listing signals kills nothing, and the operand of -l is a signal or an exit status, never a process.
		if given.isConstant && (given.text == "-l" || given.text == "-L" || given.text == "--list" ||
			given.text == "--table") {
			return false
		}
	}
	operandsOnly := false
	for index := 0; index < len(arguments); index++ {
		given := arguments[index]
		if !given.isConstant {
			// An argument only known at run time may be an option that takes the next one as its value, so the next
			// one is not judged either.
			if !operandsOnly {
				index++
			}
			continue
		}
		if operandsOnly {
			if !isInteger(given.text) {
				return true
			}
			continue
		}
		switch given.text {
		case "--":
			operandsOnly = true
			continue
		case "-s", "--signal", "-n", "-q", "--queue":
			index++
			continue
		case "--timeout":
			// util-linux's --timeout takes a time and then a signal.
			index += 2
			continue
		}
		if strings.HasPrefix(given.text, "-") {
			continue
		}
		if !isInteger(given.text) {
			return true
		}
	}
	return false
}

// isInteger is whether text is a pid, or a negative one, which names a process group.
func isInteger(text string) bool {
	_, parseError := strconv.Atoi(text)
	return parseError == nil
}

// shellScript is the script a shell's -c runs: the argument after the first option cluster holding c, such as -c,
// -ec or -lc. Options are read until the first operand, skipping the value of -o and -O, since an argument after
// the operand is the script's own and a -c there is no shell option. The script is known only when it is constant.
func shellScript(arguments []argument) (string, bool) {
	for index := 0; index < len(arguments); index++ {
		given := arguments[index]
		if !given.isConstant {
			return "", false
		}
		switch given.text {
		case "-o", "+o", "-O", "+O":
			index++
			continue
		}
		cluster, isOption := strings.CutPrefix(given.text, "-")
		if !isOption {
			// A + option turns something off and is still an option; anything else is the operand, a script file.
			if strings.HasPrefix(given.text, "+") {
				continue
			}
			return "", false
		}
		if strings.HasPrefix(cluster, "-") || !strings.Contains(cluster, "c") {
			continue
		}
		if index+1 >= len(arguments) || !arguments[index+1].isConstant {
			return "", false
		}
		return arguments[index+1].text, true
	}
	return "", false
}

// scriptKiller is the pkill or killall a shell script runs, when it holds one as a word followed by its arguments.
// The word stands alone when nothing that continues a name comes before it, so mypkill and kill-all-pkill are other
// programs, while a path like /usr/bin/pkill is the same one.
func scriptKiller(script string) (string, bool) {
	for _, killer := range []string{"pkill", "killall"} {
		for offset := 0; offset < len(script); {
			found := strings.Index(script[offset:], killer)
			if found < 0 {
				break
			}
			start := offset + found
			end := start + len(killer)
			offset = end
			if start > 0 && continuesName(script[start-1]) {
				continue
			}
			if end < len(script) && (script[end] == ' ' || script[end] == '\t') {
				return killer, true
			}
		}
	}
	return "", false
}

// continuesName is whether character can be part of a program's name, so a killer's name right after it is the end
// of some longer name.
func continuesName(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.'
}

// report flags the program argument, naming the program that stops processes by name.
func report(pass *analysis.Pass, program ast.Expr, killer string) {
	pass.Report(analysis.Diagnostic{
		Pos:      program.Pos(),
		End:      program.End(),
		Category: killByNameMessage.Id,
		Message:  killByNameMessage.Render(map[string]string{"program": killer}),
	})
}
