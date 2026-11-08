// Package rules is the list of rules the Go engine runs: one place that names every rule, so the engine,
// `--rules` and the docs all read the same list.
package rules

import (
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/suite/vet"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"
	"honnef.co/go/tools/unused"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_no_buffered_wait_delay"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_no_kill_by_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_require_escape_hatch_reason"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_ambiguous_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_any"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_boolean_outcome"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_console"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_dash_punctuation"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_else_return"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_empty_function"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_grouped_var"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_label"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_lone_block"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_lonely_if"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_parameter_reassignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_property_alias"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_redundant_import_alias"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_screaming_snake_case"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_shouting"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_stuttering_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_undefined_initializer"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_uppercase_initialism"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_use_before_define"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_useless_concatenation"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_utils_folder"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_warning_comment"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_default_last"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_error_clause"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_matching_file_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_octal_prefix"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_one_type_per_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_operator_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_separated_folder_words"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_caller_data_mutation"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_identical_branches"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_process_exit_after_output"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_write_only_collection"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_checked_assertion"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_declared_walk"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_handled_error"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_invariant_reason"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_response_status_check"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_wrapped_error"
	"github.com/system-inc/cohere/internal/go_engine/rules/nil_ness"
	"github.com/system-inc/cohere/internal/go_engine/rules/security_no_interpolated_shell_command"
	"github.com/system-inc/cohere/internal/go_engine/rules/security_no_interpolated_sql_string"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_machine_path"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_timing_assertion"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_uncached_run"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_require_failure_path"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_require_parallel"
	"github.com/system-inc/cohere/internal/go_engine/rules/unused_write"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/appends"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/assign"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/atomic"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/bools"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/cgo_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/composite"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/copy_lock"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/defers"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/directive"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/errors_as"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/frame_pointer"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/host_port"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/http_response"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/iface_assert"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/lost_cancel"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/nil_func"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/scanner_error"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/shift"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/slog"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/sql_rows_err"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/std_methods"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/string_int_conv"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/struct_tag"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/tests"
)

// syntaxOnly names the passes that read no type information, decided by reading each pass's source at
// x/tools v0.49.0 and staticcheck v0.8.1, and still true at v0.50.0, which go.mod requires: the four vet passes
// below are byte-identical between the two. Every other pass in vet's suite reads pass.TypesInfo, the ports of
// nilness and unusedwrite read staticcheck's intermediate form built from it, and staticcheck's checks read it
// through the helpers they share (analysis/code, pattern, buildir). A pass missing from this table counts as
// type aware, so a pass a newer release adds is never described as running without types before someone has
// read it.
var syntaxOnly = map[string]bool{
	// buildtag reads build constraint lines from the files' comments and text.
	"buildtag": true,
	// directive reads `//go:debug` lines from the files' comments and text.
	"directive": true,
	// framepointer reads assembly files, and of the package only its import path, which a load without
	// types also has.
	"framepointer": true,
	// unreachable walks the syntax tree for statements after a terminating one.
	"unreachable": true,
	// SA9009 reads comments for a directive indented out of effect.
	"SA9009": true,
	// ST1001 reads import declarations for a dot import, and of a file only its name, to allow one in a test.
	"ST1001": true,
	// ST1018 reads string literals for control and format characters.
	"ST1018": true,
	// ST1019 reads import declarations for a package imported twice.
	"ST1019": true,
}

// stylecheckRuledOn is the ST checks ruled on for cohere, from a census of what each found in cohere's own
// code. ST1016 is among them although staticcheck leaves it off by default.
//
// Ruled off: ST1003, whose initialisms (`ID`, `JSON`) contradict the house's `Id` and `Json` and whose
// underscores are this repository's package names, ST1005, and ST1015, whose default-in-the-middle finding is the
// house rule cohere-go/consistency-require-default-last's, so one misplaced default would be two findings (and
// ST1015 also accepts a default first, which the house rule does not).
//
// Off until ruled: ST1000, ST1020, ST1021 and ST1022, the form of package and exported declarations'
// comments, and ST1023, a declared type the right-hand side would infer. Comment form and declared types
// are style no one has judged yet.
var stylecheckRuledOn = map[string]bool{
	"ST1001": true,
	"ST1006": true,
	"ST1008": true,
	"ST1011": true,
	"ST1012": true,
	"ST1013": true,
	"ST1016": true,
	"ST1017": true,
	"ST1018": true,
	"ST1019": true,
}

// All is every rule the Go engine knows, in the order `--rules` lists them before sorting.
//
// vet's suite comes first, read from vet.Suite rather than retyped, so `cohere` in a Go repository is
// never weaker than `go vet`, and a newer x/tools that adds a pass adds its rule here with no edit. Then
// two of x/tools' own passes that vet leaves out, which find real bugs: a nil dereference or a comparison
// with nil whose answer is already known, and a write to a struct or array nothing reads again. Each runs as
// our port onto staticcheck's intermediate form, under the original's name, so the run builds one program form
// where x/tools' would build a second for these two alone (#pn1xvnd). Then
// staticcheck's SA and S checks under their own codes, the ST checks ruled on, and U1000 through an
// analyzer that reports what it finds. staticcheck's QF checks are quick fixes an editor offers, not
// findings.
//
// An SA or S check staticcheck leaves off by default (its documentation's NonDefault) is left off here
// too, until it is ruled on: at staticcheck v0.8.1 that is SA9003, an empty if or else branch. And a check
// that makes the same judgment as a vet pass already registered is left out (duplicatesOfVet).
// duplicatesOfVet names each staticcheck check that judges exactly what a registered vet pass judges, and
// that pass. Both registered, one mistake is two findings, and turning the vet rule off in the settings, or
// withholding it with //cohere:ignore, would leave its twin reporting the very thing the reader just
// silenced. vet's pass is kept, since vet is what every Go programmer already runs.
var duplicatesOfVet = map[string]string{
	// Printf format verbs that do not match their arguments.
	"SA5009": "printf",
}

// ports is each borrowed check we have ported as ours (#pn1xvnd), keyed by the original's name: a vet pass's name
// (#z2x1c7g) or a staticcheck check's code (#d8s4rsy), which never collide. A port enters this table only in its own
// landing, once it matches its original finding for finding, and All then runs it in the original's place: under the
// original's name and in the original's order, so `cohere-go/<name>`, its settings and every `//cohere:ignore`
// naming it read the same as before. The originals stay in the tree as the ports' oracles, read only by parity tests,
// and every loop below still walks them, so a check is chosen, left off or ruled on exactly as before it was ported.
var ports = map[string]*analysis.Analyzer{
	"appends":       appends.Analyzer,
	"assign":        assign.Analyzer,
	"atomic":        atomic.Analyzer,
	"bools":         bools.Analyzer,
	"cgocall":       cgo_call.Analyzer,
	"composites":    composite.Analyzer,
	"copylocks":     copy_lock.Analyzer,
	"defers":        defers.Analyzer,
	"directive":     directive.Analyzer,
	"errorsas":      errors_as.Analyzer,
	"framepointer":  frame_pointer.Analyzer,
	"hostport":      host_port.Analyzer,
	"httpresponse":  http_response.Analyzer,
	"ifaceassert":   iface_assert.Analyzer,
	"lostcancel":    lost_cancel.Analyzer,
	"nilfunc":       nil_func.Analyzer,
	"printf":        printf.Analyzer,
	"scannererr":    scanner_error.Analyzer,
	"shift":         shift.Analyzer,
	"slog":          slog.Analyzer,
	"sqlrowserr":    sql_rows_err.Analyzer,
	"stdmethods":    std_methods.Analyzer,
	"stringintconv": string_int_conv.Analyzer,
	"structtag":     struct_tag.Analyzer,
	"tests":         tests.Analyzer,
}

// portOrOriginal is the analyzer All runs for one borrowed check: its port when table holds one, and the original
// otherwise.
func portOrOriginal(original *analysis.Analyzer, table map[string]*analysis.Analyzer) *analysis.Analyzer {
	if port, ported := table[original.Name]; ported {
		return port
	}
	return original
}

func All() []rule.Rule {
	registered := []rule.Rule{}
	for _, analyzer := range vet.Suite {
		registered = append(registered, portedRule(portOrOriginal(analyzer, ports), rule.OriginVet))
	}
	registered = append(registered, portedRule(nil_ness.Analyzer, rule.OriginTools))
	registered = append(registered, portedRule(unused_write.Analyzer, rule.OriginTools))
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers) {
		if wrapped.Doc != nil && wrapped.Doc.NonDefault || duplicatesOfVet[wrapped.Analyzer.Name] != "" {
			continue
		}
		registered = append(registered, portedRule(portOrOriginal(wrapped.Analyzer, ports), rule.OriginStaticcheck))
	}
	for _, wrapped := range stylecheck.Analyzers {
		if stylecheckRuledOn[wrapped.Analyzer.Name] {
			registered = append(registered, portedRule(portOrOriginal(wrapped.Analyzer, ports), rule.OriginStaticcheck))
		}
	}
	registered = append(registered, rule.Rule{
		Name:         rule.Namespace + unused.Analyzer.Analyzer.Name,
		Origin:       rule.OriginStaticcheck,
		UpstreamName: unused.Analyzer.Analyzer.Name,
		TypeAware:    true,
		Analyzer:     unusedReport,
	})
	// The house rules come last, each from its own package, which holds its name, origin and analyzer
	// together so the registry restates none of them.
	registered = append(registered, House()...)
	return registered
}

// House is every house rule, in the order the registry lists them after the ported ones.
func House() []rule.Rule {
	return []rule.Rule{
		concurrency_no_buffered_wait_delay.Rule,
		concurrency_no_kill_by_name.Rule,
		concurrency_require_escape_hatch_reason.Rule,
		consistency_no_abbreviated_identifier.Rule,
		consistency_no_ambiguous_identifier.Rule,
		consistency_no_any.Rule,
		consistency_no_boolean_outcome.Rule,
		consistency_no_console.Rule,
		consistency_no_dash_punctuation.Rule,
		consistency_no_else_return.Rule,
		consistency_no_empty_function.Rule,
		consistency_no_grouped_var.Rule,
		consistency_no_label.Rule,
		consistency_no_lone_block.Rule,
		consistency_no_lonely_if.Rule,
		consistency_no_long_file.Rule,
		consistency_no_long_line.Rule,
		consistency_no_parameter_reassignment.Rule,
		consistency_no_property_alias.Rule,
		consistency_no_redundant_import_alias.Rule,
		consistency_no_screaming_snake_case.Rule,
		consistency_no_shouting.Rule,
		consistency_no_stuttering_name.Rule,
		consistency_no_undefined_initializer.Rule,
		consistency_no_uppercase_initialism.Rule,
		consistency_no_use_before_define.Rule,
		consistency_no_useless_concatenation.Rule,
		consistency_no_utils_folder.Rule,
		consistency_no_warning_comment.Rule,
		consistency_require_default_last.Rule,
		consistency_require_error_clause.Rule,
		consistency_require_matching_file_name.Rule,
		consistency_require_octal_prefix.Rule,
		consistency_require_one_type_per_file.Rule,
		consistency_require_operator_assignment.Rule,
		consistency_require_separated_folder_words.Rule,
		correctness_no_caller_data_mutation.Rule,
		correctness_no_identical_branches.Rule,
		correctness_no_process_exit_after_output.Rule,
		correctness_no_write_only_collection.Rule,
		correctness_require_checked_assertion.Rule,
		correctness_require_declared_walk.Rule,
		correctness_require_handled_error.Rule,
		correctness_require_invariant_reason.Rule,
		correctness_require_response_status_check.Rule,
		correctness_require_wrapped_error.Rule,
		security_no_interpolated_shell_command.Rule,
		security_no_interpolated_sql_string.Rule,
		testing_no_machine_path.Rule,
		testing_no_timing_assertion.Rule,
		testing_no_uncached_run.Rule,
		testing_require_failure_path.Rule,
		testing_require_parallel.Rule,
	}
}

// portedRule is the rule that runs an upstream analyzer under the name it has upstream.
func portedRule(analyzer *analysis.Analyzer, origin rule.Origin) rule.Rule {
	return rule.Rule{
		Name:         rule.Namespace + analyzer.Name,
		Origin:       origin,
		UpstreamName: analyzer.Name,
		TypeAware:    !syntaxOnly[analyzer.Name],
		Analyzer:     analyzer,
	}
}
