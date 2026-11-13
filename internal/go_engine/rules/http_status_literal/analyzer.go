// Package http_status_literal is staticcheck's `ST1013`, an HTTP status code written as a number, such as 506, where
// net/http names it, http.StatusVariantAlsoNegotiates, in the argument of a net/http function that takes one, ported
// onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1013/st1013.go, whose messages and fixes
// it reports word for word, at the argument, with no range option, and withholds in a generated file as the
// original's FilterGenerated does. The original finds its calls with staticcheck's pattern package, `(CallExpr (Symbol
// name@(Or "net/http.Error" ...)) args)`, which it looks up in x/tools' type index, every call whose callee is one of
// the four functions. The port hears every call on the dispatcher's walk instead and asks staticcheck_code.SymbolIndex,
// the kit's translation of the pattern's Symbol of an Or, of the call's function, so it requires neither the type index
// nor the inspector of its own the original's pattern matching asks for. The type index finds a call through
// parentheses and a qualifying package, as SymbolIndex does, and neither function is generic, so the two find the
// same calls. Like the original, it reads the codes left alone from staticcheck's config pass, whose
// http_status_code_whitelist a staticcheck.conf can set, by default 200, 400, 404 and 500. It reads the pass as the
// engine runs it (staticcheck_configuration), which applies the staticcheck.conf of a project under the user's cache
// directory where the original's never does, so there alone the port leaves alone what the original reports.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package http_status_literal

import (
	"fmt"
	"go/ast"
	"go/constant"
	"slices"
	"strconv"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/config"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
	"github.com/system-inc/cohere/internal/go_engine/staticcheck_configuration"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1013"

// Doc is the original's title.
const Doc = "Should use constants for HTTP error codes, not magic numbers"

// statusArgument is one net/http function that takes a status code, and which of its arguments the code is.
type statusArgument struct {
	function string
	index    int
}

// statusArguments are the functions the original reads, each by its full name, in the order its pattern lists them.
var statusArguments = []statusArgument{
	{function: "net/http.Error", index: 2},
	{function: "net/http.Redirect", index: 3},
	{function: "net/http.StatusText", index: 0},
	{function: "net/http.RedirectHandler", index: 1},
}

// statusFunctions are the full names of statusArguments' functions, in their order, the names of the pattern's Or.
var statusFunctions = func() []string {
	functions := make([]string, 0, len(statusArguments))
	for _, argument := range statusArguments {
		functions = append(functions, argument.function)
	}
	return functions
}()

// statusConstants are the codes net/http names, each with its constant's name, the original's table as it stands,
// which lacks three net/http has since named, 103, 421 and 425, so neither reports them.
var statusConstants = map[int64]string{
	100: "StatusContinue",
	101: "StatusSwitchingProtocols",
	102: "StatusProcessing",
	200: "StatusOK",
	201: "StatusCreated",
	202: "StatusAccepted",
	203: "StatusNonAuthoritativeInfo",
	204: "StatusNoContent",
	205: "StatusResetContent",
	206: "StatusPartialContent",
	207: "StatusMultiStatus",
	208: "StatusAlreadyReported",
	226: "StatusIMUsed",
	300: "StatusMultipleChoices",
	301: "StatusMovedPermanently",
	302: "StatusFound",
	303: "StatusSeeOther",
	304: "StatusNotModified",
	305: "StatusUseProxy",
	307: "StatusTemporaryRedirect",
	308: "StatusPermanentRedirect",
	400: "StatusBadRequest",
	401: "StatusUnauthorized",
	402: "StatusPaymentRequired",
	403: "StatusForbidden",
	404: "StatusNotFound",
	405: "StatusMethodNotAllowed",
	406: "StatusNotAcceptable",
	407: "StatusProxyAuthRequired",
	408: "StatusRequestTimeout",
	409: "StatusConflict",
	410: "StatusGone",
	411: "StatusLengthRequired",
	412: "StatusPreconditionFailed",
	413: "StatusRequestEntityTooLarge",
	414: "StatusRequestURITooLong",
	415: "StatusUnsupportedMediaType",
	416: "StatusRequestedRangeNotSatisfiable",
	417: "StatusExpectationFailed",
	418: "StatusTeapot",
	422: "StatusUnprocessableEntity",
	423: "StatusLocked",
	424: "StatusFailedDependency",
	426: "StatusUpgradeRequired",
	428: "StatusPreconditionRequired",
	429: "StatusTooManyRequests",
	431: "StatusRequestHeaderFieldsTooLarge",
	451: "StatusUnavailableForLegalReasons",
	500: "StatusInternalServerError",
	501: "StatusNotImplemented",
	502: "StatusBadGateway",
	503: "StatusServiceUnavailable",
	504: "StatusGatewayTimeout",
	505: "StatusHTTPVersionNotSupported",
	506: "StatusVariantAlsoNegotiates",
	507: "StatusInsufficientStorage",
	508: "StatusLoopDetected",
	510: "StatusNotExtended",
	511: "StatusNetworkAuthenticationRequired",
}

// Listener is what the port hears of each package from the dispatcher: every call, each judged by the function it
// calls, and staticcheck's configuration, which lists the codes left alone.
var Listener = rule.Listener{
	FileLocal: true,
	Requires:  []*analysis.Analyzer{staticcheck_configuration.Analyzer},
	Nodes:     []ast.Node{(*ast.CallExpr)(nil)},
	Node:      checkCall,
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// checkCall reports the status code call passes as a number, when call is to one of the functions the original reads,
// the code is written as an integer literal, net/http names it, and the configuration does not leave it alone.
func checkCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	index := statusIndex(context, call)
	// A call through a tuple, http.Error(responseAndCode()), passes fewer arguments than the code's index, and the
	// original reads none of it.
	if index < 0 || index >= len(call.Args) {
		return
	}
	literal := call.Args[index]
	typeAndValue, isLiteral := staticcheck_code.IntegerLiteral(context, literal)
	if !isLiteral {
		return
	}
	code, isExact := constant.Int64Val(typeAndValue.Value)
	if !isExact {
		return
	}
	configuration, isConfiguration := context.ResultOf(staticcheck_configuration.Analyzer).(*config.Config)
	if !isConfiguration {
		// Invariant: the config pass's result type is *config.Config, which the checker holds it to, so any other is
		// a mistake in building the pass, and the dispatcher's recover makes it this rule's crash on the package.
		panic(fmt.Sprintf("reading %s's result: it is %T, where *config.Config was expected",
			staticcheck_configuration.Analyzer.Name, context.ResultOf(staticcheck_configuration.Analyzer)))
	}
	if slices.Contains(configuration.HTTPStatusCodeWhitelist, strconv.FormatInt(code, 10)) {
		return
	}
	constantName, isNamed := statusConstants[code]
	if !isNamed {
		return
	}
	staticcheck_report.Report(context, literal,
		fmt.Sprintf("should use constant http.%s instead of numeric literal %d", constantName, code),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(analysis.SuggestedFix{
			Message: fmt.Sprintf("Use http.%s instead of %d", constantName, code),
			// The fix writes the package as http whatever the file imports it as, as the original's does.
			TextEdits: []analysis.TextEdit{{
				Pos:     literal.Pos(),
				End:     literal.End(),
				NewText: []byte("http." + constantName),
			}},
		}))
}

// statusIndex is which argument of call is the status code, when call is to one of the functions the original reads,
// and -1 when it is to none of them. It asks staticcheck_code.SymbolIndex, the pattern's Symbol of an Or, which
// resolves the call's function once for all four names.
func statusIndex(context *rule.Context, call *ast.CallExpr) int {
	function := staticcheck_code.SymbolIndex(context, call.Fun, statusFunctions...)
	if function < 0 {
		return -1
	}
	return statusArguments[function].index
}
