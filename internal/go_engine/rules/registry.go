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

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/address_compared_to_nil"
	"github.com/system-inc/cohere/internal/go_engine/rules/argument_overwritten_before_use"
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
	"github.com/system-inc/cohere/internal/go_engine/rules/discarded_pure_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/dot_import"
	"github.com/system-inc/cohere/internal/go_engine/rules/double_negation"
	"github.com/system-inc/cohere/internal/go_engine/rules/duplicate_build_constraint"
	"github.com/system-inc/cohere/internal/go_engine/rules/duplicate_import"
	"github.com/system-inc/cohere/internal/go_engine/rules/duration_unit_suffix"
	"github.com/system-inc/cohere/internal/go_engine/rules/error_variable_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/extreme_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/http_status_literal"
	"github.com/system-inc/cohere/internal/go_engine/rules/identical_operands"
	"github.com/system-inc/cohere/internal/go_engine/rules/impossible_target_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/inconsistent_receiver_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_bitwise_operation"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_break"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_copy"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_field_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_random_integer"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_sort"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_url_query_modification"
	"github.com/system-inc/cohere/internal/go_engine/rules/integer_division_to_zero"
	"github.com/system-inc/cohere/internal/go_engine/rules/invisible_character"
	"github.com/system-inc/cohere/internal/go_engine/rules/math_on_converted_integer"
	"github.com/system-inc/cohere/internal/go_engine/rules/misplaced_error_result"
	"github.com/system-inc/cohere/internal/go_engine/rules/modulo_by_one"
	"github.com/system-inc/cohere/internal/go_engine/rules/nan_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/negative_length_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/negative_zero_float"
	"github.com/system-inc/cohere/internal/go_engine/rules/never_nil_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/nil_ness"
	"github.com/system-inc/cohere/internal/go_engine/rules/receiver_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/repeated_if_else_condition"
	"github.com/system-inc/cohere/internal/go_engine/rules/security_no_interpolated_shell_command"
	"github.com/system-inc/cohere/internal/go_engine/rules/security_no_interpolated_sql_string"
	"github.com/system-inc/cohere/internal/go_engine/rules/self_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/single_argument_append"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_machine_path"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_timing_assertion"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_uncached_run"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_require_failure_path"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_require_parallel"
	"github.com/system-inc/cohere/internal/go_engine/rules/typed_nil_interface_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/unchanging_loop_condition"
	"github.com/system-inc/cohere/internal/go_engine/rules/unconditional_loop_exit"
	"github.com/system-inc/cohere/internal/go_engine/rules/unobserved_append"
	"github.com/system-inc/cohere/internal/go_engine/rules/unreachable_type_case"
	"github.com/system-inc/cohere/internal/go_engine/rules/unread_assigned_value"
	"github.com/system-inc/cohere/internal/go_engine/rules/unused_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/unused_write"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/appends"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/asm_decl"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/assign"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/atomic"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/bools"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/build_tag"
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
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/loop_closure"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/lost_cancel"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/nil_func"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/scanner_error"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/shift"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/signal_channel_analyzer"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/slog"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/sql_rows_err"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/std_methods"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/std_version"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/string_int_conv"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/struct_tag"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/testing_goroutine"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/tests"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/time_format"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/unmarshal"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/unreachable"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/unsafe_ptr"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/unused_result"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/wait_group"
	"github.com/system-inc/cohere/internal/go_engine/rules/yoda_condition"
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
// staticcheck's SA and S checks under their own codes, the ST checks ruled on, and U1000 as our port,
// which reports what it finds where the original returns its verdict for staticcheck's runner to report
// (#7hdcn3w). staticcheck's QF checks are quick fixes an editor offers, not findings.
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
// A port that listens on a dispatcher carries its listener beside its analyzer, so its rule registers with it, and is
// validated and held to its dispatcher as every dispatched rule is.
var ports = map[string]port{
	"SA4000": {analyzer: identical_operands.Analyzer, listener: &identical_operands.Listener},
	"SA4001": {analyzer: ineffective_copy.Analyzer, listener: &ineffective_copy.Listener},
	"SA4003": {analyzer: extreme_comparison.Analyzer, listener: &extreme_comparison.Listener},
	"SA4004": {analyzer: unconditional_loop_exit.Analyzer, listener: &unconditional_loop_exit.Listener},
	"SA4005": {analyzer: ineffective_field_assignment.Analyzer, listener: &ineffective_field_assignment.Listener},
	"SA4006": {analyzer: unread_assigned_value.Analyzer, listener: &unread_assigned_value.Listener},
	"SA4008": {analyzer: unchanging_loop_condition.Analyzer, listener: &unchanging_loop_condition.Listener},
	"SA4009": {analyzer: argument_overwritten_before_use.Analyzer, listener: &argument_overwritten_before_use.Listener},
	"SA4010": {analyzer: unobserved_append.Analyzer, listener: &unobserved_append.Listener},
	"SA4011": {analyzer: ineffective_break.Analyzer, listener: &ineffective_break.Listener},
	"SA4012": {analyzer: nan_comparison.Analyzer, listener: &nan_comparison.Listener},
	"SA4013": {analyzer: double_negation.Analyzer, listener: &double_negation.Listener},
	"SA4014": {analyzer: repeated_if_else_condition.Analyzer, listener: &repeated_if_else_condition.Listener},
	"SA4015": {analyzer: math_on_converted_integer.Analyzer, listener: &math_on_converted_integer.Listener},
	"SA4016": {analyzer: ineffective_bitwise_operation.Analyzer, listener: &ineffective_bitwise_operation.Listener},
	"SA4017": {analyzer: discarded_pure_call.Analyzer, listener: &discarded_pure_call.Listener},
	"SA4018": {analyzer: self_assignment.Analyzer, listener: &self_assignment.Listener},
	"SA4019": {analyzer: duplicate_build_constraint.Analyzer, listener: &duplicate_build_constraint.Listener},
	"SA4020": {analyzer: unreachable_type_case.Analyzer, listener: &unreachable_type_case.Listener},
	"SA4021": {analyzer: single_argument_append.Analyzer, listener: &single_argument_append.Listener},
	"SA4022": {analyzer: address_compared_to_nil.Analyzer, listener: &address_compared_to_nil.Listener},
	"SA4023": {analyzer: typed_nil_interface_comparison.Analyzer, listener: &typed_nil_interface_comparison.Listener},
	"SA4024": {analyzer: negative_length_comparison.Analyzer, listener: &negative_length_comparison.Listener},
	"SA4025": {analyzer: integer_division_to_zero.Analyzer, listener: &integer_division_to_zero.Listener},
	"SA4026": {analyzer: negative_zero_float.Analyzer, listener: &negative_zero_float.Listener},
	"SA4027": {
		analyzer: ineffective_url_query_modification.Analyzer,
		listener: &ineffective_url_query_modification.Listener,
	},
	"SA4028": {analyzer: modulo_by_one.Analyzer, listener: &modulo_by_one.Listener},
	"SA4029": {analyzer: ineffective_sort.Analyzer, listener: &ineffective_sort.Listener},
	"SA4030": {analyzer: ineffective_random_integer.Analyzer, listener: &ineffective_random_integer.Listener},
	"SA4031": {analyzer: never_nil_comparison.Analyzer, listener: &never_nil_comparison.Listener},
	"SA4032": {
		analyzer: impossible_target_comparison.Analyzer,
		listener: &impossible_target_comparison.Listener,
	},
	"ST1001":           {analyzer: dot_import.Analyzer, listener: &dot_import.Listener},
	"ST1006":           {analyzer: receiver_name.Analyzer, listener: &receiver_name.Listener},
	"ST1008":           {analyzer: misplaced_error_result.Analyzer, listener: &misplaced_error_result.Listener},
	"ST1011":           {analyzer: duration_unit_suffix.Analyzer, listener: &duration_unit_suffix.Listener},
	"ST1012":           {analyzer: error_variable_name.Analyzer, listener: &error_variable_name.Listener},
	"ST1013":           {analyzer: http_status_literal.Analyzer, listener: &http_status_literal.Listener},
	"ST1016":           {analyzer: inconsistent_receiver_name.Analyzer, listener: &inconsistent_receiver_name.Listener},
	"ST1017":           {analyzer: yoda_condition.Analyzer, listener: &yoda_condition.Listener},
	"ST1018":           {analyzer: invisible_character.Analyzer, listener: &invisible_character.Listener},
	"ST1019":           {analyzer: duplicate_import.Analyzer, listener: &duplicate_import.Listener},
	"appends":          {analyzer: appends.Analyzer, listener: &appends.Listener},
	"asmdecl":          {analyzer: asm_decl.Analyzer},
	"assign":           {analyzer: assign.Analyzer},
	"atomic":           {analyzer: atomic.Analyzer, listener: &atomic.Listener},
	"bools":            {analyzer: bools.Analyzer},
	"buildtag":         {analyzer: build_tag.Analyzer},
	"cgocall":          {analyzer: cgo_call.Analyzer},
	"composites":       {analyzer: composite.Analyzer, listener: &composite.Listener},
	"copylocks":        {analyzer: copy_lock.Analyzer, listener: &copy_lock.Listener},
	"defers":           {analyzer: defers.Analyzer, listener: &defers.Listener},
	"directive":        {analyzer: directive.Analyzer},
	"errorsas":         {analyzer: errors_as.Analyzer, listener: &errors_as.Listener},
	"framepointer":     {analyzer: frame_pointer.Analyzer},
	"hostport":         {analyzer: host_port.Analyzer},
	"httpresponse":     {analyzer: http_response.Analyzer},
	"ifaceassert":      {analyzer: iface_assert.Analyzer, listener: &iface_assert.Listener},
	"loopclosure":      {analyzer: loop_closure.Analyzer},
	"lostcancel":       {analyzer: lost_cancel.Analyzer},
	"nilfunc":          {analyzer: nil_func.Analyzer, listener: &nil_func.Listener},
	"printf":           {analyzer: printf.Analyzer},
	"scannererr":       {analyzer: scanner_error.Analyzer},
	"shift":            {analyzer: shift.Analyzer},
	"sigchanyzer":      {analyzer: signal_channel_analyzer.Analyzer},
	"slog":             {analyzer: slog.Analyzer, listener: &slog.Listener},
	"sqlrowserr":       {analyzer: sql_rows_err.Analyzer},
	"stdmethods":       {analyzer: std_methods.Analyzer, listener: &std_methods.Listener},
	"stdversion":       {analyzer: std_version.Analyzer},
	"stringintconv":    {analyzer: string_int_conv.Analyzer, listener: &string_int_conv.Listener},
	"structtag":        {analyzer: struct_tag.Analyzer, listener: &struct_tag.Listener},
	"testinggoroutine": {analyzer: testing_goroutine.Analyzer},
	"tests":            {analyzer: tests.Analyzer},
	"timeformat":       {analyzer: time_format.Analyzer, listener: &time_format.Listener},
	"unmarshal":        {analyzer: unmarshal.Analyzer, listener: &unmarshal.Listener},
	"unreachable":      {analyzer: unreachable.Analyzer},
	"unsafeptr":        {analyzer: unsafe_ptr.Analyzer, listener: &unsafe_ptr.Listener},
	"unusedresult":     {analyzer: unused_result.Analyzer},
	"waitgroup":        {analyzer: wait_group.Analyzer, listener: &wait_group.Listener},
}

// port is a borrowed check's port as the registry runs it: its analyzer, and the listener that analyzer reads its
// findings from, nil for a port that walks the package itself.
type port struct {
	analyzer *analysis.Analyzer
	listener *rule.Listener
}

// portOrOriginal is what All runs for one borrowed check: its port when table holds one, and the original, which
// listens on no dispatcher, otherwise.
func portOrOriginal(original *analysis.Analyzer, table map[string]port) port {
	if ported, held := table[original.Name]; held {
		return ported
	}
	return port{analyzer: original}
}

func All() []rule.Rule {
	registered := []rule.Rule{}
	for _, analyzer := range vet.Suite {
		registered = append(registered, borrowedRule(portOrOriginal(analyzer, ports), rule.OriginVet))
	}
	registered = append(registered, dispatchedRule(nil_ness.Analyzer, &nil_ness.Listener, rule.OriginTools))
	registered = append(registered, dispatchedRule(unused_write.Analyzer, &unused_write.Listener, rule.OriginTools))
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers) {
		if wrapped.Doc != nil && wrapped.Doc.NonDefault || duplicatesOfVet[wrapped.Analyzer.Name] != "" {
			continue
		}
		registered = append(registered, borrowedRule(portOrOriginal(wrapped.Analyzer, ports), rule.OriginStaticcheck))
	}
	for _, wrapped := range stylecheck.Analyzers {
		if stylecheckRuledOn[wrapped.Analyzer.Name] {
			chosen := portOrOriginal(wrapped.Analyzer, ports)
			registered = append(registered, borrowedRule(chosen, rule.OriginStaticcheck))
		}
	}
	registered = append(registered, rule.Rule{
		Name:         rule.Namespace + unused_code.Analyzer.Name,
		Origin:       rule.OriginStaticcheck,
		UpstreamName: unused_code.Analyzer.Name,
		TypeAware:    true,
		Analyzer:     unused_code.Analyzer,
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

// dispatchedRule is the rule that runs an upstream pass's port, whose analyzer reports what a dispatcher's walk found
// for listener, under the name it has upstream.
func dispatchedRule(analyzer *analysis.Analyzer, listener *rule.Listener, origin rule.Origin) rule.Rule {
	return borrowedRule(port{analyzer: analyzer, listener: listener}, origin)
}

// borrowedRule is the rule that runs chosen, a borrowed check's port or its original, under the name it has upstream,
// with the port's listener when it listens on a dispatcher.
func borrowedRule(chosen port, origin rule.Origin) rule.Rule {
	registered := portedRule(chosen.analyzer, origin)
	registered.Listener = chosen.listener
	return registered
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
