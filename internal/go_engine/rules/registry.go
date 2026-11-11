// Package rules is the list of rules the Go engine runs: one place that names every rule, so the engine,
// `--rules` and the docs all read the same list.
package rules

import (
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/suite/vet"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/address_compared_to_nil"
	"github.com/system-inc/cohere/internal/go_engine/rules/argument_overwritten_before_use"
	"github.com/system-inc/cohere/internal/go_engine/rules/assertion_to_own_type"
	"github.com/system-inc/cohere/internal/go_engine/rules/assigned_benchmark_iterations"
	"github.com/system-inc/cohere/internal/go_engine/rules/boolean_constant_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/boolean_return_of_condition"
	"github.com/system-inc/cohere/internal/go_engine/rules/buffer_string_conversion"
	"github.com/system-inc/cohere/internal/go_engine/rules/byte_slice_write_string"
	"github.com/system-inc/cohere/internal/go_engine/rules/bytes_compare_equality"
	"github.com/system-inc/cohere/internal/go_engine/rules/case_insensitive_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_no_buffered_wait_delay"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_no_kill_by_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_require_escape_hatch_reason"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_require_joined_pipe_reader"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_file_name"
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
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_utils_file_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_utils_folder"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_warning_comment"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_default_last"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_error_clause"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_matching_file_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_octal_prefix"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_one_type_per_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_operator_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_separated_folder_words"
	"github.com/system-inc/cohere/internal/go_engine/rules/constant_group_type"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_caller_data_mutation"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_identical_branches"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_process_exit_after_output"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_no_write_only_collection"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_checked_assertion"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_declared_walk"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_drained_child_output"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_handled_error"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_invariant_reason"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_response_status_check"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_wrapped_error"
	"github.com/system-inc/cohere/internal/go_engine/rules/cyclic_finalizer"
	"github.com/system-inc/cohere/internal/go_engine/rules/defer_in_channel_range_loop"
	"github.com/system-inc/cohere/internal/go_engine/rules/defer_in_infinite_loop"
	"github.com/system-inc/cohere/internal/go_engine/rules/deferred_lock_after_lock"
	"github.com/system-inc/cohere/internal/go_engine/rules/deprecated_use"
	"github.com/system-inc/cohere/internal/go_engine/rules/discarded_pure_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/dot_import"
	"github.com/system-inc/cohere/internal/go_engine/rules/double_negation"
	"github.com/system-inc/cohere/internal/go_engine/rules/duplicate_build_constraint"
	"github.com/system-inc/cohere/internal/go_engine/rules/duplicate_import"
	"github.com/system-inc/cohere/internal/go_engine/rules/duplicate_trim_character"
	"github.com/system-inc/cohere/internal/go_engine/rules/duration_unit_suffix"
	"github.com/system-inc/cohere/internal/go_engine/rules/dynamic_printf_format"
	"github.com/system-inc/cohere/internal/go_engine/rules/early_deferred_close"
	"github.com/system-inc/cohere/internal/go_engine/rules/elaborate_sleep"
	"github.com/system-inc/cohere/internal/go_engine/rules/empty_critical_section"
	"github.com/system-inc/cohere/internal/go_engine/rules/empty_select_default"
	"github.com/system-inc/cohere/internal/go_engine/rules/error_variable_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/errors_new_of_sprintf"
	"github.com/system-inc/cohere/internal/go_engine/rules/extreme_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/failed_type_assertion_read"
	"github.com/system-inc/cohere/internal/go_engine/rules/find_all_zero_limit"
	"github.com/system-inc/cohere/internal/go_engine/rules/guarded_delete"
	"github.com/system-inc/cohere/internal/go_engine/rules/guarded_map_access"
	"github.com/system-inc/cohere/internal/go_engine/rules/hoisted_map_key_conversion"
	"github.com/system-inc/cohere/internal/go_engine/rules/http_status_literal"
	"github.com/system-inc/cohere/internal/go_engine/rules/identical_operands"
	"github.com/system-inc/cohere/internal/go_engine/rules/impossible_target_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/impossible_type_assertion"
	"github.com/system-inc/cohere/internal/go_engine/rules/inappropriate_context_key"
	"github.com/system-inc/cohere/internal/go_engine/rules/inconsistent_receiver_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/index_compared_as_contains"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_bitwise_operation"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_break"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_copy"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_field_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_random_integer"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_sort"
	"github.com/system-inc/cohere/internal/go_engine/rules/ineffective_url_query_modification"
	"github.com/system-inc/cohere/internal/go_engine/rules/infinite_loop_condition"
	"github.com/system-inc/cohere/internal/go_engine/rules/infinite_recursion"
	"github.com/system-inc/cohere/internal/go_engine/rules/integer_division_to_zero"
	"github.com/system-inc/cohere/internal/go_engine/rules/invalid_listen_address"
	"github.com/system-inc/cohere/internal/go_engine/rules/invalid_regular_expression"
	"github.com/system-inc/cohere/internal/go_engine/rules/invalid_string_conversion_argument"
	"github.com/system-inc/cohere/internal/go_engine/rules/invalid_struct_tag"
	"github.com/system-inc/cohere/internal/go_engine/rules/invalid_template"
	"github.com/system-inc/cohere/internal/go_engine/rules/invalid_time_layout"
	"github.com/system-inc/cohere/internal/go_engine/rules/invalid_url"
	"github.com/system-inc/cohere/internal/go_engine/rules/invalid_utf8_cut_set"
	"github.com/system-inc/cohere/internal/go_engine/rules/invisible_character"
	"github.com/system-inc/cohere/internal/go_engine/rules/ip_compared_as_bytes"
	"github.com/system-inc/cohere/internal/go_engine/rules/leaky_time_tick"
	"github.com/system-inc/cohere/internal/go_engine/rules/loop_copy"
	"github.com/system-inc/cohere/internal/go_engine/rules/manual_trim"
	"github.com/system-inc/cohere/internal/go_engine/rules/marshaled_struct_without_exported_fields"
	"github.com/system-inc/cohere/internal/go_engine/rules/math_on_converted_integer"
	"github.com/system-inc/cohere/internal/go_engine/rules/misaligned_atomic_field"
	"github.com/system-inc/cohere/internal/go_engine/rules/misplaced_error_result"
	"github.com/system-inc/cohere/internal/go_engine/rules/modified_writer_buffer"
	"github.com/system-inc/cohere/internal/go_engine/rules/modulo_by_one"
	"github.com/system-inc/cohere/internal/go_engine/rules/nan_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/nanosecond_sleep"
	"github.com/system-inc/cohere/internal/go_engine/rules/negative_length_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/negative_zero_float"
	"github.com/system-inc/cohere/internal/go_engine/rules/never_nil_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/nil_context_argument"
	"github.com/system-inc/cohere/internal/go_engine/rules/nil_map_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/nil_ness"
	"github.com/system-inc/cohere/internal/go_engine/rules/non_canonical_header_key"
	"github.com/system-inc/cohere/internal/go_engine/rules/non_octal_file_mode"
	"github.com/system-inc/cohere/internal/go_engine/rules/non_pointer_pool_value"
	"github.com/system-inc/cohere/internal/go_engine/rules/non_pointer_unmarshal"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
	"github.com/system-inc/cohere/internal/go_engine/rules/overlapping_encode_buffers"
	"github.com/system-inc/cohere/internal/go_engine/rules/oversized_shift"
	"github.com/system-inc/cohere/internal/go_engine/rules/print_of_sprintf"
	"github.com/system-inc/cohere/internal/go_engine/rules/range_over_rune_conversion"
	"github.com/system-inc/cohere/internal/go_engine/rules/range_over_string_directly"
	"github.com/system-inc/cohere/internal/go_engine/rules/receiver_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/redundant_assertion_nil_check"
	"github.com/system-inc/cohere/internal/go_engine/rules/redundant_canonical_header_key"
	"github.com/system-inc/cohere/internal/go_engine/rules/redundant_control_flow"
	"github.com/system-inc/cohere/internal/go_engine/rules/redundant_make_arguments"
	"github.com/system-inc/cohere/internal/go_engine/rules/redundant_nil_check_around_loop"
	"github.com/system-inc/cohere/internal/go_engine/rules/redundant_nil_check_on_length"
	"github.com/system-inc/cohere/internal/go_engine/rules/redundant_slice_length"
	"github.com/system-inc/cohere/internal/go_engine/rules/redundant_sprintf_of_string"
	"github.com/system-inc/cohere/internal/go_engine/rules/regular_expression_match_in_loop"
	"github.com/system-inc/cohere/internal/go_engine/rules/regular_expression_raw_string"
	"github.com/system-inc/cohere/internal/go_engine/rules/removed_user_directory"
	"github.com/system-inc/cohere/internal/go_engine/rules/repeated_if_else_condition"
	"github.com/system-inc/cohere/internal/go_engine/rules/replace_zero_count"
	"github.com/system-inc/cohere/internal/go_engine/rules/reversed_errors_is_arguments"
	"github.com/system-inc/cohere/internal/go_engine/rules/security_no_interpolated_shell_command"
	"github.com/system-inc/cohere/internal/go_engine/rules/security_no_interpolated_sql_string"
	"github.com/system-inc/cohere/internal/go_engine/rules/self_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/separate_declaration_and_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/shell_command_as_program"
	"github.com/system-inc/cohere/internal/go_engine/rules/single_append_concatenation"
	"github.com/system-inc/cohere/internal/go_engine/rules/single_argument_append"
	"github.com/system-inc/cohere/internal/go_engine/rules/single_case_select"
	"github.com/system-inc/cohere/internal/go_engine/rules/sliding_copy"
	"github.com/system-inc/cohere/internal/go_engine/rules/sort_helper_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/sort_slice_on_non_slice"
	"github.com/system-inc/cohere/internal/go_engine/rules/spaced_compiler_directive"
	"github.com/system-inc/cohere/internal/go_engine/rules/spinning_empty_loop"
	"github.com/system-inc/cohere/internal/go_engine/rules/struct_conversion_by_field_copy"
	"github.com/system-inc/cohere/internal/go_engine/rules/swapped_seek_arguments"
	"github.com/system-inc/cohere/internal/go_engine/rules/test_failure_in_goroutine"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_machine_path"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_timing_assertion"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_uncached_run"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_require_failure_path"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_require_parallel"
	"github.com/system-inc/cohere/internal/go_engine/rules/time_since"
	"github.com/system-inc/cohere/internal/go_engine/rules/time_until"
	"github.com/system-inc/cohere/internal/go_engine/rules/timer_reset_result_used"
	"github.com/system-inc/cohere/internal/go_engine/rules/type_switch_bound_value"
	"github.com/system-inc/cohere/internal/go_engine/rules/typed_nil_interface_comparison"
	"github.com/system-inc/cohere/internal/go_engine/rules/unbuffered_signal_channel"
	"github.com/system-inc/cohere/internal/go_engine/rules/uncalled_deferred_function"
	"github.com/system-inc/cohere/internal/go_engine/rules/unchanging_loop_condition"
	"github.com/system-inc/cohere/internal/go_engine/rules/unconditional_loop_exit"
	"github.com/system-inc/cohere/internal/go_engine/rules/unencodable_binary_value"
	"github.com/system-inc/cohere/internal/go_engine/rules/unexiting_test_main"
	"github.com/system-inc/cohere/internal/go_engine/rules/unnecessary_blank_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/unnecessary_sprint"
	"github.com/system-inc/cohere/internal/go_engine/rules/unobserved_append"
	"github.com/system-inc/cohere/internal/go_engine/rules/unreachable_type_case"
	"github.com/system-inc/cohere/internal/go_engine/rules/unread_assigned_value"
	"github.com/system-inc/cohere/internal/go_engine/rules/unsupported_marshal_type"
	"github.com/system-inc/cohere/internal/go_engine/rules/untrappable_signal"
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
	"github.com/system-inc/cohere/internal/go_engine/rules/wait_group_add_in_goroutine"
	"github.com/system-inc/cohere/internal/go_engine/rules/yoda_condition"
)

// syntaxOnly names the passes that read no type information, decided by reading each pass's source at
// x/tools v0.49.0 and staticcheck v0.8.1, and still true at v0.50.0, which go.mod requires: the four vet passes
// below are byte-identical between the two. Every other pass in vet's suite reads pass.TypesInfo, the ports of
// nilness and unusedwrite read the engine's own intermediate representation built from it, and staticcheck's checks
// read it through the helpers they share (analysis/code, pattern, buildir). A pass missing from this table counts as
// type aware, so a pass a newer release adds is never described as running without types before someone has read it.
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
// our port onto the engine's own intermediate representation, under the original's name, so the run builds one
// program form where x/tools' would build a second for these two alone (#pn1xvnd). Then
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
// naming it read the same as before. The originals stay in the tree as the ports' oracles, read only by parity tests.
// The loops below walk staticcheck's checks through the registry's own tables of them (staticcheckChecks), held to
// the suites by a test, so a check is chosen, left off or ruled on exactly as before it was ported, and the binary
// links none of the originals.
// A port that listens on a dispatcher carries its listener beside its analyzer, so its rule registers with it, and is
// validated and held to its dispatcher as every dispatched rule is.
var ports = map[string]port{
	"S1000": {analyzer: single_case_select.Analyzer, listener: &single_case_select.Listener},
	"S1001": {analyzer: loop_copy.Analyzer, listener: &loop_copy.Listener},
	"S1002": {analyzer: boolean_constant_comparison.Analyzer, listener: &boolean_constant_comparison.Listener},
	"S1003": {analyzer: index_compared_as_contains.Analyzer, listener: &index_compared_as_contains.Listener},
	"S1004": {analyzer: bytes_compare_equality.Analyzer, listener: &bytes_compare_equality.Listener},
	"S1005": {analyzer: unnecessary_blank_identifier.Analyzer, listener: &unnecessary_blank_identifier.Listener},
	"S1006": {analyzer: infinite_loop_condition.Analyzer, listener: &infinite_loop_condition.Listener},
	"S1007": {analyzer: regular_expression_raw_string.Analyzer, listener: &regular_expression_raw_string.Listener},
	"S1008": {analyzer: boolean_return_of_condition.Analyzer, listener: &boolean_return_of_condition.Listener},
	"S1009": {analyzer: redundant_nil_check_on_length.Analyzer, listener: &redundant_nil_check_on_length.Listener},
	"S1010": {analyzer: redundant_slice_length.Analyzer, listener: &redundant_slice_length.Listener},
	"S1011": {analyzer: single_append_concatenation.Analyzer, listener: &single_append_concatenation.Listener},
	"S1012": {analyzer: time_since.Analyzer, listener: &time_since.Listener},
	"S1016": {analyzer: struct_conversion_by_field_copy.Analyzer, listener: &struct_conversion_by_field_copy.Listener},
	"S1017": {analyzer: manual_trim.Analyzer, listener: &manual_trim.Listener},
	"S1018": {analyzer: sliding_copy.Analyzer, listener: &sliding_copy.Listener},
	"S1019": {analyzer: redundant_make_arguments.Analyzer, listener: &redundant_make_arguments.Listener},
	"S1020": {analyzer: redundant_assertion_nil_check.Analyzer, listener: &redundant_assertion_nil_check.Listener},
	"S1021": {
		analyzer: separate_declaration_and_assignment.Analyzer,
		listener: &separate_declaration_and_assignment.Listener,
	},
	"S1023":  {analyzer: redundant_control_flow.Analyzer, listener: &redundant_control_flow.Listener},
	"S1024":  {analyzer: time_until.Analyzer, listener: &time_until.Listener},
	"S1025":  {analyzer: redundant_sprintf_of_string.Analyzer, listener: &redundant_sprintf_of_string.Listener},
	"S1028":  {analyzer: errors_new_of_sprintf.Analyzer, listener: &errors_new_of_sprintf.Listener},
	"S1029":  {analyzer: range_over_string_directly.Analyzer, listener: &range_over_string_directly.Listener},
	"S1030":  {analyzer: buffer_string_conversion.Analyzer, listener: &buffer_string_conversion.Listener},
	"S1031":  {analyzer: redundant_nil_check_around_loop.Analyzer, listener: &redundant_nil_check_around_loop.Listener},
	"S1032":  {analyzer: sort_helper_call.Analyzer, listener: &sort_helper_call.Listener},
	"S1033":  {analyzer: guarded_delete.Analyzer, listener: &guarded_delete.Listener},
	"S1034":  {analyzer: type_switch_bound_value.Analyzer, listener: &type_switch_bound_value.Listener},
	"S1035":  {analyzer: redundant_canonical_header_key.Analyzer, listener: &redundant_canonical_header_key.Listener},
	"S1036":  {analyzer: guarded_map_access.Analyzer, listener: &guarded_map_access.Listener},
	"S1037":  {analyzer: elaborate_sleep.Analyzer, listener: &elaborate_sleep.Listener},
	"S1038":  {analyzer: print_of_sprintf.Analyzer, listener: &print_of_sprintf.Listener},
	"S1039":  {analyzer: unnecessary_sprint.Analyzer, listener: &unnecessary_sprint.Listener},
	"S1040":  {analyzer: assertion_to_own_type.Analyzer, listener: &assertion_to_own_type.Listener},
	"SA1000": {analyzer: invalid_regular_expression.Analyzer, listener: &invalid_regular_expression.Listener},
	"SA1001": {analyzer: invalid_template.Analyzer, listener: &invalid_template.Listener},
	"SA1002": {analyzer: invalid_time_layout.Analyzer, listener: &invalid_time_layout.Listener},
	"SA1003": {analyzer: unencodable_binary_value.Analyzer, listener: &unencodable_binary_value.Listener},
	"SA1004": {analyzer: nanosecond_sleep.Analyzer, listener: &nanosecond_sleep.Listener},
	"SA1005": {analyzer: shell_command_as_program.Analyzer, listener: &shell_command_as_program.Listener},
	"SA1006": {analyzer: dynamic_printf_format.Analyzer, listener: &dynamic_printf_format.Listener},
	"SA1007": {analyzer: invalid_url.Analyzer, listener: &invalid_url.Listener},
	"SA1008": {analyzer: non_canonical_header_key.Analyzer, listener: &non_canonical_header_key.Listener},
	"SA1010": {analyzer: find_all_zero_limit.Analyzer, listener: &find_all_zero_limit.Listener},
	"SA1011": {analyzer: invalid_utf8_cut_set.Analyzer, listener: &invalid_utf8_cut_set.Listener},
	"SA1012": {analyzer: nil_context_argument.Analyzer, listener: &nil_context_argument.Listener},
	"SA1013": {analyzer: swapped_seek_arguments.Analyzer, listener: &swapped_seek_arguments.Listener},
	"SA1014": {analyzer: non_pointer_unmarshal.Analyzer, listener: &non_pointer_unmarshal.Listener},
	"SA1015": {analyzer: leaky_time_tick.Analyzer, listener: &leaky_time_tick.Listener},
	"SA1016": {analyzer: untrappable_signal.Analyzer, listener: &untrappable_signal.Listener},
	"SA1017": {analyzer: unbuffered_signal_channel.Analyzer, listener: &unbuffered_signal_channel.Listener},
	"SA1018": {analyzer: replace_zero_count.Analyzer, listener: &replace_zero_count.Listener},
	"SA1019": {analyzer: deprecated_use.Analyzer, listener: &deprecated_use.Listener},
	"SA1020": {analyzer: invalid_listen_address.Analyzer, listener: &invalid_listen_address.Listener},
	"SA1021": {analyzer: ip_compared_as_bytes.Analyzer, listener: &ip_compared_as_bytes.Listener},
	"SA1023": {analyzer: modified_writer_buffer.Analyzer, listener: &modified_writer_buffer.Listener},
	"SA1024": {analyzer: duplicate_trim_character.Analyzer, listener: &duplicate_trim_character.Listener},
	"SA1025": {analyzer: timer_reset_result_used.Analyzer, listener: &timer_reset_result_used.Listener},
	"SA1026": {analyzer: unsupported_marshal_type.Analyzer, listener: &unsupported_marshal_type.Listener},
	"SA1027": {analyzer: misaligned_atomic_field.Analyzer, listener: &misaligned_atomic_field.Listener},
	"SA1028": {analyzer: sort_slice_on_non_slice.Analyzer, listener: &sort_slice_on_non_slice.Listener},
	"SA1029": {analyzer: inappropriate_context_key.Analyzer, listener: &inappropriate_context_key.Listener},
	"SA1030": {
		analyzer: invalid_string_conversion_argument.Analyzer,
		listener: &invalid_string_conversion_argument.Listener,
	},
	"SA1031": {analyzer: overlapping_encode_buffers.Analyzer, listener: &overlapping_encode_buffers.Listener},
	"SA1032": {analyzer: reversed_errors_is_arguments.Analyzer, listener: &reversed_errors_is_arguments.Listener},
	"SA2000": {analyzer: wait_group_add_in_goroutine.Analyzer, listener: &wait_group_add_in_goroutine.Listener},
	"SA2001": {analyzer: empty_critical_section.Analyzer, listener: &empty_critical_section.Listener},
	"SA2002": {analyzer: test_failure_in_goroutine.Analyzer, listener: &test_failure_in_goroutine.Listener},
	"SA2003": {analyzer: deferred_lock_after_lock.Analyzer, listener: &deferred_lock_after_lock.Listener},
	"SA3000": {analyzer: unexiting_test_main.Analyzer, listener: &unexiting_test_main.Listener},
	"SA3001": {analyzer: assigned_benchmark_iterations.Analyzer, listener: &assigned_benchmark_iterations.Listener},
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
	"SA5000": {analyzer: nil_map_assignment.Analyzer, listener: &nil_map_assignment.Listener},
	"SA5001": {analyzer: early_deferred_close.Analyzer, listener: &early_deferred_close.Listener},
	"SA5002": {analyzer: spinning_empty_loop.Analyzer, listener: &spinning_empty_loop.Listener},
	"SA5003": {analyzer: defer_in_infinite_loop.Analyzer, listener: &defer_in_infinite_loop.Listener},
	"SA5004": {analyzer: empty_select_default.Analyzer, listener: &empty_select_default.Listener},
	"SA5005": {analyzer: cyclic_finalizer.Analyzer, listener: &cyclic_finalizer.Listener},
	"SA5007": {analyzer: infinite_recursion.Analyzer, listener: &infinite_recursion.Listener},
	"SA5008": {analyzer: invalid_struct_tag.Analyzer, listener: &invalid_struct_tag.Listener},
	"SA5010": {analyzer: impossible_type_assertion.Analyzer, listener: &impossible_type_assertion.Listener},
	"SA5012": {analyzer: odd_slice_argument.Analyzer},
	"SA6000": {
		analyzer: regular_expression_match_in_loop.Analyzer,
		listener: &regular_expression_match_in_loop.Listener,
	},
	"SA6001": {analyzer: hoisted_map_key_conversion.Analyzer, listener: &hoisted_map_key_conversion.Listener},
	"SA6002": {analyzer: non_pointer_pool_value.Analyzer, listener: &non_pointer_pool_value.Listener},
	"SA6003": {analyzer: range_over_rune_conversion.Analyzer, listener: &range_over_rune_conversion.Listener},
	"SA6005": {
		analyzer: case_insensitive_comparison.Analyzer,
		listener: &case_insensitive_comparison.Listener,
	},
	"SA6006": {analyzer: byte_slice_write_string.Analyzer, listener: &byte_slice_write_string.Listener},
	"SA9001": {analyzer: defer_in_channel_range_loop.Analyzer, listener: &defer_in_channel_range_loop.Listener},
	"SA9002": {analyzer: non_octal_file_mode.Analyzer, listener: &non_octal_file_mode.Listener},
	"SA9004": {analyzer: constant_group_type.Analyzer, listener: &constant_group_type.Listener},
	"SA9005": {
		analyzer: marshaled_struct_without_exported_fields.Analyzer,
		listener: &marshaled_struct_without_exported_fields.Listener,
	},
	"SA9006":           {analyzer: oversized_shift.Analyzer, listener: &oversized_shift.Listener},
	"SA9007":           {analyzer: removed_user_directory.Analyzer, listener: &removed_user_directory.Listener},
	"SA9008":           {analyzer: failed_type_assertion_read.Analyzer, listener: &failed_type_assertion_read.Listener},
	"SA9009":           {analyzer: spaced_compiler_directive.Analyzer, listener: &spaced_compiler_directive.Listener},
	"SA9010":           {analyzer: uncalled_deferred_function.Analyzer, listener: &uncalled_deferred_function.Listener},
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
	"assign":           {analyzer: assign.Analyzer, listener: &assign.Listener},
	"atomic":           {analyzer: atomic.Analyzer, listener: &atomic.Listener},
	"bools":            {analyzer: bools.Analyzer, listener: &bools.Listener},
	"buildtag":         {analyzer: build_tag.Analyzer},
	"cgocall":          {analyzer: cgo_call.Analyzer},
	"composites":       {analyzer: composite.Analyzer, listener: &composite.Listener},
	"copylocks":        {analyzer: copy_lock.Analyzer, listener: &copy_lock.Listener},
	"defers":           {analyzer: defers.Analyzer, listener: &defers.Listener},
	"directive":        {analyzer: directive.Analyzer},
	"errorsas":         {analyzer: errors_as.Analyzer, listener: &errors_as.Listener},
	"framepointer":     {analyzer: frame_pointer.Analyzer},
	"hostport":         {analyzer: host_port.Analyzer, listener: &host_port.Listener},
	"httpresponse":     {analyzer: http_response.Analyzer, listener: &http_response.Listener},
	"ifaceassert":      {analyzer: iface_assert.Analyzer, listener: &iface_assert.Listener},
	"loopclosure":      {analyzer: loop_closure.Analyzer, listener: &loop_closure.Listener},
	"lostcancel":       {analyzer: lost_cancel.Analyzer, listener: &lost_cancel.Listener},
	"nilfunc":          {analyzer: nil_func.Analyzer, listener: &nil_func.Listener},
	"printf":           {analyzer: printf.Analyzer},
	"scannererr":       {analyzer: scanner_error.Analyzer, listener: &scanner_error.Listener},
	"shift":            {analyzer: shift.Analyzer, listener: &shift.Listener},
	"sigchanyzer":      {analyzer: signal_channel_analyzer.Analyzer, listener: &signal_channel_analyzer.Listener},
	"slog":             {analyzer: slog.Analyzer, listener: &slog.Listener},
	"sqlrowserr":       {analyzer: sql_rows_err.Analyzer, listener: &sql_rows_err.Listener},
	"stdmethods":       {analyzer: std_methods.Analyzer, listener: &std_methods.Listener},
	"stdversion":       {analyzer: std_version.Analyzer, listener: &std_version.Listener},
	"stringintconv":    {analyzer: string_int_conv.Analyzer, listener: &string_int_conv.Listener},
	"structtag":        {analyzer: struct_tag.Analyzer, listener: &struct_tag.Listener},
	"testinggoroutine": {analyzer: testing_goroutine.Analyzer, listener: &testing_goroutine.Listener},
	"tests":            {analyzer: tests.Analyzer, listener: &tests.Listener},
	"timeformat":       {analyzer: time_format.Analyzer, listener: &time_format.Listener},
	"unmarshal":        {analyzer: unmarshal.Analyzer, listener: &unmarshal.Listener},
	"unreachable":      {analyzer: unreachable.Analyzer, listener: &unreachable.Listener},
	"unsafeptr":        {analyzer: unsafe_ptr.Analyzer, listener: &unsafe_ptr.Listener},
	"unusedresult":     {analyzer: unused_result.Analyzer, listener: &unused_result.Listener},
	"waitgroup":        {analyzer: wait_group.Analyzer, listener: &wait_group.Listener},
}

// port is a borrowed check's port as the registry runs it: its analyzer, and the listener that analyzer reads its
// findings from, nil for a port that walks the package itself.
type port struct {
	analyzer *analysis.Analyzer
	listener *rule.Listener
}

// portOrOriginal is what All runs for one of vet's passes: its port when table holds one, and the original, which
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
	for _, check := range registeredStaticcheckChecks() {
		registered = append(registered, staticcheckRule(check, portOf(check)))
	}
	registered = append(registered, rule.Rule{
		Name:         rule.Namespace + unused_code.Analyzer.Name,
		Origin:       rule.OriginStaticcheck,
		UpstreamName: unused_code.Analyzer.Name,
		TypeAware:    true,
		Analyzer:     unused_code.Analyzer,
		// Staticcheck's runner merges U1000 this way by hand rather than from its documentation
		// (honnef.co/go/tools@v0.8.1 lintcmd/lint.go:264): code is unused only if it is unused in every build.
		MergeIfAll: true,
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
		concurrency_require_joined_pipe_reader.Rule,
		consistency_no_abbreviated_file_name.Rule,
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
		consistency_no_utils_file_name.Rule,
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
		correctness_require_drained_child_output.Rule,
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

// registeredStaticcheckChecks is staticcheck's checks All registers, in its order: every SA and S check staticcheck
// runs by default that judges what no registered vet pass already judges, and then the ST checks ruled on.
func registeredStaticcheckChecks() []staticcheckCheck {
	registered := []staticcheckCheck{}
	for _, check := range staticcheckChecks {
		if !check.nonDefault && duplicatesOfVet[check.name] == "" {
			registered = append(registered, check)
		}
	}
	for _, check := range stylecheckChecks {
		if stylecheckRuledOn[check.name] {
			registered = append(registered, check)
		}
	}
	return registered
}

// portOf is the port All runs for check, one of staticcheck's checks it registers.
func portOf(check staticcheckCheck) port {
	ported, held := ports[check.name]
	if !held {
		// Invariant: the registry links none of staticcheck's originals, so it runs a check only through its port,
		// and TestEveryRegisteredStaticcheckCheckHasAPort holds that every check it registers has one. A check
		// without one is a mistake in building the registry, never in a package, and panicking as All runs fails
		// every test before any rule does.
		panic("staticcheck's " + check.name + " is registered with no port, and the registry runs no original")
	}
	return ported
}

// staticcheckRule is the rule that runs chosen, the port of staticcheck's check, merged across builds as the check's
// documentation says, so a port merges as its original does.
func staticcheckRule(check staticcheckCheck, chosen port) rule.Rule {
	registered := borrowedRule(chosen, rule.OriginStaticcheck)
	registered.MergeIfAll = check.mergeIfAll
	return registered
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
