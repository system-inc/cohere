package rules

import (
	"flag"
	"fmt"
	"go/ast"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/analysis/suite/vet"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
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
	"github.com/system-inc/cohere/internal/go_engine/rules/constant_group_type"
	"github.com/system-inc/cohere/internal/go_engine/rules/cyclic_finalizer"
	"github.com/system-inc/cohere/internal/go_engine/rules/defer_in_channel_range_loop"
	"github.com/system-inc/cohere/internal/go_engine/rules/defer_in_infinite_loop"
	"github.com/system-inc/cohere/internal/go_engine/rules/deferred_lock_after_lock"
	"github.com/system-inc/cohere/internal/go_engine/rules/deprecated_use"
	"github.com/system-inc/cohere/internal/go_engine/rules/discarded_pure_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
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
	"github.com/system-inc/cohere/internal/go_engine/rules/wait_group_add_in_goroutine"
	"github.com/system-inc/cohere/internal/go_engine/rules/yoda_condition"
)

// registry is All() built once, for every test in the package to read.
var registry = All()

// registryProblems is every reason a list of rules could not be the registry: a rule that does not
// validate, its listener included, or a name two rules share, which would make a finding's rule ambiguous and its
// config key configure two rules at once.
func registryProblems(registered []rule.Rule) []string {
	problems := []string{}
	seen := map[string]bool{}
	for _, registeredRule := range registered {
		if validateError := registeredRule.Validate(); validateError != nil {
			problems = append(problems, validateError.Error())
		}
		if seen[registeredRule.Name] {
			problems = append(problems, fmt.Sprintf("rule %q is registered twice", registeredRule.Name))
		}
		seen[registeredRule.Name] = true
	}
	return problems
}

// registeredRuleNamed finds a rule in the registry by the name a finding prints.
func registeredRuleNamed(name string) (rule.Rule, bool) {
	for _, registeredRule := range registry {
		if registeredRule.Name == name {
			return registeredRule, true
		}
	}
	return rule.Rule{}, false
}

// TestEveryRegisteredRuleValidatesAndHasItsOwnName holds the registry to well-formed, uniquely named
// rules. The check also runs over a planted list, to prove it can fail.
func TestEveryRegisteredRuleValidatesAndHasItsOwnName(test *testing.T) {
	test.Parallel()
	for _, problem := range registryProblems(registry) {
		test.Error(problem)
	}

	planted := &analysis.Analyzer{
		Name: "planted",
		Doc:  "planted finds nothing; it exists so a rule under test has an analyzer",
		Run: func(pass *analysis.Pass) (any, error) {
			return nil, nil
		},
	}
	plantedRegistry := []rule.Rule{
		{Name: "cohere-go/printf", Origin: rule.OriginVet, UpstreamName: "printf", Analyzer: planted},
		{Name: "cohere-go/consistency-no-long-line", Origin: rule.OriginHouse, UpstreamName: "lll", Analyzer: planted},
		{Name: "cohere-go/printf", Origin: rule.OriginVet, UpstreamName: "printf", Analyzer: planted},
		{Name: "cohere-go/U1000", Origin: rule.OriginStaticcheck, UpstreamName: "U1000", Analyzer: planted,
			Listener: &rule.Listener{FileEnter: plantedFileEnter, PackageExit: plantedPackageExit}},
		{Name: "cohere-go/SA4006", Origin: rule.OriginStaticcheck, UpstreamName: "SA4006", Analyzer: planted,
			Listener: &rule.Listener{FileEnter: plantedFileEnter,
				IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: plantedFunction}}},
		{Name: "cohere-go/nilness", Origin: rule.OriginTools, UpstreamName: "nilness", Analyzer: planted,
			Listener: &rule.Listener{Requires: []*analysis.Analyzer{planted}}},
		{Name: "cohere-go/unusedwrite", Origin: rule.OriginTools, UpstreamName: "unusedwrite", Analyzer: planted,
			Listener: &rule.Listener{PackageExit: plantedPackageExit}},
	}
	plantedProblems := registryProblems(plantedRegistry)
	expected := []string{
		"a house rule has no upstream", `rule "cohere-go/printf" is registered twice`,
		"declares both", "declares no callback", "PackageExit alone",
	}
	if len(plantedProblems) != len(expected) {
		test.Fatalf(
			"the planted registry has %d problems, expected %d: %q",
			len(plantedProblems), len(expected), plantedProblems,
		)
	}
	for index, saying := range expected {
		if !strings.Contains(plantedProblems[index], saying) {
			test.Errorf("planted problem %d is %q, expected it to say %q", index, plantedProblems[index], saying)
		}
	}
}

// plantedFileEnter is a syntax callback for a planted listener, which the registry only validates.
func plantedFileEnter(*rule.Context, *ast.File) {
	// Never called: the registry reads only whether a callback is set.
}

// plantedFunction is an IR callback for a planted listener, which the registry only validates.
func plantedFunction(*rule.Context, *intermediate_representation.Function) {
	// Never called: the registry reads only whether a callback is set.
}

// plantedPackageExit is a planted listener's PackageExit, which the registry only validates.
func plantedPackageExit(*rule.Context) {
	// Never called: the registry reads only whether a callback is set.
}

// expectBlock holds the registry, from offset on, to one rule per analyzer in order, each named for its
// analyzer, of the given origin, and running that very analyzer rather than a copy that could drift.
func expectBlock(test *testing.T, offset int, analyzers []*analysis.Analyzer, origin rule.Origin) {
	test.Helper()
	if len(registry) < offset+len(analyzers) {
		test.Fatalf(
			"the registry has %d rules, too few to hold %d %s rules from rule %d",
			len(registry), len(analyzers), origin, offset,
		)
	}
	for position, analyzer := range analyzers {
		index := offset + position
		registeredRule := registry[index]
		if registeredRule.Analyzer != analyzer {
			test.Errorf("rule %d runs %q, expected %q", index, registeredRule.Analyzer.Name, analyzer.Name)
			continue
		}
		if registeredRule.Name != rule.Namespace+analyzer.Name || registeredRule.UpstreamName != analyzer.Name {
			test.Errorf(
				"%q is registered as %q with upstream %q",
				analyzer.Name, registeredRule.Name, registeredRule.UpstreamName,
			)
		}
		if registeredRule.Origin != origin {
			test.Errorf("%q has origin %q, expected %q", analyzer.Name, registeredRule.Origin, origin)
		}
	}
}

// ruledStylecheck is the ST checks ruled on, written out here rather than read from the registry's own
// table, so a check added to or dropped from that table by mistake fails the test.
var ruledStylecheck = []string{
	"ST1001", "ST1006", "ST1008", "ST1011", "ST1012", "ST1013", "ST1016", "ST1017", "ST1018", "ST1019",
}

// portedStylecheck is each ST check switched to its port, with the port, written out here rather than read from
// ports, so a line dropped from or wrongly mapped in that table fails TestEveryPortedStylecheckRunsItsPort: a test
// that read ports would expect whatever the table says and pass with the original back in place. One check a line.
var portedStylecheck = []struct {
	name string
	port *analysis.Analyzer
}{
	{"ST1001", dot_import.Analyzer},
	{"ST1006", receiver_name.Analyzer},
	{"ST1008", misplaced_error_result.Analyzer},
	{"ST1011", duration_unit_suffix.Analyzer},
	{"ST1012", error_variable_name.Analyzer},
	{"ST1013", http_status_literal.Analyzer},
	{"ST1016", inconsistent_receiver_name.Analyzer},
	{"ST1017", yoda_condition.Analyzer},
	{"ST1018", invisible_character.Analyzer},
	{"ST1019", duplicate_import.Analyzer},
}

// portedStaticcheck is each of staticcheck's SA and S checks switched to its port, with the port, written out here
// rather than read from ports, so a line dropped from or wrongly mapped in that table fails
// TestEveryPortedStaticcheckRunsItsPort: a test that read ports would expect whatever the table says and pass with the
// original back in place. One check a line; each family's switch adds its own lines.
var portedStaticcheck = []struct {
	name string
	port *analysis.Analyzer
}{
	{"S1000", single_case_select.Analyzer},
	{"S1001", loop_copy.Analyzer},
	{"S1002", boolean_constant_comparison.Analyzer},
	{"S1003", index_compared_as_contains.Analyzer},
	{"S1004", bytes_compare_equality.Analyzer},
	{"S1005", unnecessary_blank_identifier.Analyzer},
	{"S1006", infinite_loop_condition.Analyzer},
	{"S1007", regular_expression_raw_string.Analyzer},
	{"S1008", boolean_return_of_condition.Analyzer},
	{"S1009", redundant_nil_check_on_length.Analyzer},
	{"S1010", redundant_slice_length.Analyzer},
	{"S1011", single_append_concatenation.Analyzer},
	{"S1012", time_since.Analyzer},
	{"S1016", struct_conversion_by_field_copy.Analyzer},
	{"S1017", manual_trim.Analyzer},
	{"S1018", sliding_copy.Analyzer},
	{"S1019", redundant_make_arguments.Analyzer},
	{"S1020", redundant_assertion_nil_check.Analyzer},
	{"S1021", separate_declaration_and_assignment.Analyzer},
	{"S1023", redundant_control_flow.Analyzer},
	{"S1024", time_until.Analyzer},
	{"S1025", redundant_sprintf_of_string.Analyzer},
	{"S1028", errors_new_of_sprintf.Analyzer},
	{"S1029", range_over_string_directly.Analyzer},
	{"S1030", buffer_string_conversion.Analyzer},
	{"S1031", redundant_nil_check_around_loop.Analyzer},
	{"S1032", sort_helper_call.Analyzer},
	{"S1033", guarded_delete.Analyzer},
	{"S1034", type_switch_bound_value.Analyzer},
	{"S1035", redundant_canonical_header_key.Analyzer},
	{"S1036", guarded_map_access.Analyzer},
	{"S1037", elaborate_sleep.Analyzer},
	{"S1038", print_of_sprintf.Analyzer},
	{"S1039", unnecessary_sprint.Analyzer},
	{"S1040", assertion_to_own_type.Analyzer},
	{"SA1000", invalid_regular_expression.Analyzer},
	{"SA1001", invalid_template.Analyzer},
	{"SA1002", invalid_time_layout.Analyzer},
	{"SA1003", unencodable_binary_value.Analyzer},
	{"SA1004", nanosecond_sleep.Analyzer},
	{"SA1005", shell_command_as_program.Analyzer},
	{"SA1006", dynamic_printf_format.Analyzer},
	{"SA1007", invalid_url.Analyzer},
	{"SA1008", non_canonical_header_key.Analyzer},
	{"SA1010", find_all_zero_limit.Analyzer},
	{"SA1011", invalid_utf8_cut_set.Analyzer},
	{"SA1012", nil_context_argument.Analyzer},
	{"SA1013", swapped_seek_arguments.Analyzer},
	{"SA1014", non_pointer_unmarshal.Analyzer},
	{"SA1015", leaky_time_tick.Analyzer},
	{"SA1016", untrappable_signal.Analyzer},
	{"SA1017", unbuffered_signal_channel.Analyzer},
	{"SA1018", replace_zero_count.Analyzer},
	{"SA1019", deprecated_use.Analyzer},
	{"SA1020", invalid_listen_address.Analyzer},
	{"SA1021", ip_compared_as_bytes.Analyzer},
	{"SA1023", modified_writer_buffer.Analyzer},
	{"SA1024", duplicate_trim_character.Analyzer},
	{"SA1025", timer_reset_result_used.Analyzer},
	{"SA1026", unsupported_marshal_type.Analyzer},
	{"SA1027", misaligned_atomic_field.Analyzer},
	{"SA1028", sort_slice_on_non_slice.Analyzer},
	{"SA1029", inappropriate_context_key.Analyzer},
	{"SA1030", invalid_string_conversion_argument.Analyzer},
	{"SA1031", overlapping_encode_buffers.Analyzer},
	{"SA1032", reversed_errors_is_arguments.Analyzer},
	{"SA2000", wait_group_add_in_goroutine.Analyzer},
	{"SA2001", empty_critical_section.Analyzer},
	{"SA2002", test_failure_in_goroutine.Analyzer},
	{"SA2003", deferred_lock_after_lock.Analyzer},
	{"SA3000", unexiting_test_main.Analyzer},
	{"SA3001", assigned_benchmark_iterations.Analyzer},
	{"SA4000", identical_operands.Analyzer},
	{"SA4001", ineffective_copy.Analyzer},
	{"SA4003", extreme_comparison.Analyzer},
	{"SA4004", unconditional_loop_exit.Analyzer},
	{"SA4005", ineffective_field_assignment.Analyzer},
	{"SA4006", unread_assigned_value.Analyzer},
	{"SA4008", unchanging_loop_condition.Analyzer},
	{"SA4009", argument_overwritten_before_use.Analyzer},
	{"SA4010", unobserved_append.Analyzer},
	{"SA4011", ineffective_break.Analyzer},
	{"SA4012", nan_comparison.Analyzer},
	{"SA4013", double_negation.Analyzer},
	{"SA4014", repeated_if_else_condition.Analyzer},
	{"SA4015", math_on_converted_integer.Analyzer},
	{"SA4016", ineffective_bitwise_operation.Analyzer},
	{"SA4017", discarded_pure_call.Analyzer},
	{"SA4018", self_assignment.Analyzer},
	{"SA4019", duplicate_build_constraint.Analyzer},
	{"SA4020", unreachable_type_case.Analyzer},
	{"SA4021", single_argument_append.Analyzer},
	{"SA4022", address_compared_to_nil.Analyzer},
	{"SA4023", typed_nil_interface_comparison.Analyzer},
	{"SA4024", negative_length_comparison.Analyzer},
	{"SA4025", integer_division_to_zero.Analyzer},
	{"SA4026", negative_zero_float.Analyzer},
	{"SA4027", ineffective_url_query_modification.Analyzer},
	{"SA4028", modulo_by_one.Analyzer},
	{"SA4029", ineffective_sort.Analyzer},
	{"SA4030", ineffective_random_integer.Analyzer},
	{"SA4031", never_nil_comparison.Analyzer},
	{"SA4032", impossible_target_comparison.Analyzer},
	{"SA5000", nil_map_assignment.Analyzer},
	{"SA5001", early_deferred_close.Analyzer},
	{"SA5002", spinning_empty_loop.Analyzer},
	{"SA5003", defer_in_infinite_loop.Analyzer},
	{"SA5004", empty_select_default.Analyzer},
	{"SA5005", cyclic_finalizer.Analyzer},
	{"SA5007", infinite_recursion.Analyzer},
	{"SA5008", invalid_struct_tag.Analyzer},
	{"SA5010", impossible_type_assertion.Analyzer},
	{"SA5012", odd_slice_argument.Analyzer},
	{"SA6000", regular_expression_match_in_loop.Analyzer},
	{"SA6001", hoisted_map_key_conversion.Analyzer},
	{"SA6002", non_pointer_pool_value.Analyzer},
	{"SA6003", range_over_rune_conversion.Analyzer},
	{"SA6005", case_insensitive_comparison.Analyzer},
	{"SA6006", byte_slice_write_string.Analyzer},
	{"SA9001", defer_in_channel_range_loop.Analyzer},
	{"SA9002", non_octal_file_mode.Analyzer},
	{"SA9004", constant_group_type.Analyzer},
	{"SA9005", marshaled_struct_without_exported_fields.Analyzer},
	{"SA9006", oversized_shift.Analyzer},
	{"SA9007", removed_user_directory.Analyzer},
	{"SA9008", failed_type_assertion_read.Analyzer},
	{"SA9009", spaced_compiler_directive.Analyzer},
	{"SA9010", uncalled_deferred_function.Analyzer},
}

// staticcheckAnalyzers is every SA and S check staticcheck runs by default, in its order, which is the order
// they follow the x/tools passes in, each as the registry runs it: its port when ports holds one.
func staticcheckAnalyzers() []*analysis.Analyzer {
	analyzers := []*analysis.Analyzer{}
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers) {
		if wrapped.Doc != nil && wrapped.Doc.NonDefault || duplicatesOfVet[wrapped.Analyzer.Name] != "" {
			continue
		}
		analyzers = append(analyzers, portOrOriginal(wrapped.Analyzer, ports).analyzer)
	}
	return analyzers
}

// stylecheckAnalyzers is the ruled ST checks, in stylecheck's order, which is the order they follow the SA
// and S checks in, each as the registry runs it: its port when ports holds one.
func stylecheckAnalyzers() []*analysis.Analyzer {
	analyzers := []*analysis.Analyzer{}
	for _, wrapped := range stylecheck.Analyzers {
		if slices.Contains(ruledStylecheck, wrapped.Analyzer.Name) {
			analyzers = append(analyzers, portOrOriginal(wrapped.Analyzer, ports).analyzer)
		}
	}
	return analyzers
}

// TestVetsSuiteOpensTheRegistryInItsOwnOrder holds the promise that `cohere` is never weaker than
// `go vet`: every pass vet runs is a rule, under vet's name, in vet's order, running vet's own analyzer or, for a
// pass we have ported, the port that matched it finding for finding.
func TestVetsSuiteOpensTheRegistryInItsOwnOrder(test *testing.T) {
	test.Parallel()
	expected := []*analysis.Analyzer{}
	for _, pass := range vet.Suite {
		expected = append(expected, portOrOriginal(pass, ports).analyzer)
	}
	expectBlock(test, 0, expected, rule.OriginVet)
}

// portedVet is vet's passes cohere runs as its own ports, written out here rather than read from the registry's own
// table, so a port's line dropped from or added to that table by mistake fails the test below.
var portedVet = []string{
	"appends", "asmdecl", "assign", "atomic", "bools", "buildtag", "cgocall", "composites", "copylocks", "defers",
	"directive", "errorsas", "framepointer", "hostport", "httpresponse", "ifaceassert", "loopclosure", "lostcancel",
	"nilfunc", "printf", "scannererr", "shift", "sigchanyzer", "slog", "sqlrowserr", "stdmethods", "stdversion",
	"stringintconv", "structtag", "testinggoroutine", "tests", "timeformat", "unmarshal", "unreachable", "unsafeptr",
	"unusedresult", "waitgroup",
}

// TestExactlyThePortedVetPassesRunTheirPorts holds every vet pass portedVet names to a registered rule running a port,
// not vet's original, and every other vet pass to the original itself, so the registry's table can neither lose a port
// nor gain one unannounced. Each name in portedVet is a pass vet runs, so the list cannot name a port that pins
// nothing.
func TestExactlyThePortedVetPassesRunTheirPorts(test *testing.T) {
	test.Parallel()
	vetPasses := map[string]bool{}
	for _, pass := range vet.Suite {
		vetPasses[pass.Name] = true
		registeredRule, registered := registeredRuleNamed(rule.Namespace + pass.Name)
		switch {
		case !registered:
			test.Errorf("vet's %q is not registered", pass.Name)
		case slices.Contains(portedVet, pass.Name) && registeredRule.Analyzer == pass:
			test.Errorf("vet's %q runs the original, expected its port", pass.Name)
		case !slices.Contains(portedVet, pass.Name) && registeredRule.Analyzer != pass:
			test.Errorf("vet's %q runs a port that portedVet does not name", pass.Name)
		}
	}
	for _, name := range portedVet {
		if !vetPasses[name] {
			test.Errorf("portedVet names %q, which vet does not run", name)
		}
	}
}

// borrowedChecks is every borrowed check a port may stand in for, by its name: vet's passes, staticcheck's SA, S and
// ST checks, each the original.
func borrowedChecks() map[string]*analysis.Analyzer {
	originals := map[string]*analysis.Analyzer{}
	for _, pass := range vet.Suite {
		originals[pass.Name] = pass
	}
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers, stylecheck.Analyzers) {
		originals[wrapped.Analyzer.Name] = wrapped.Analyzer
	}
	return originals
}

// TestEveryPortStandsInForABorrowedCheck holds each port in ports to the check it replaces: keyed by a name vet or
// staticcheck gives a check, named that same name, so the rule and its ignores keep it, and never the original itself,
// which would port nothing. A port of a vet pass is also documented in the original's words and at its address, so
// `go vet`'s documentation describes the rule exactly as before.
func TestEveryPortStandsInForABorrowedCheck(test *testing.T) {
	test.Parallel()
	originals := borrowedChecks()
	vetPasses := map[string]bool{}
	for _, pass := range vet.Suite {
		vetPasses[pass.Name] = true
	}
	for name, ported := range ports {
		original, borrowed := originals[name]
		switch {
		case !borrowed:
			test.Errorf("ports holds a port for %q, which neither vet nor staticcheck names", name)
		case ported.analyzer == original:
			test.Errorf("ports maps %q to the original's own analyzer, so nothing is ported", name)
		case ported.analyzer.Name != name:
			test.Errorf("the port for %q is named %q, so its rule would be renamed", name,
				ported.analyzer.Name)
		case vetPasses[name] && (ported.analyzer.Doc != original.Doc || ported.analyzer.URL != original.URL):
			test.Errorf("the port for vet's %q is documented differently from the original", name)
		case !maps.Equal(flagDefaults(ported.analyzer), flagDefaults(original)):
			test.Errorf("the port for %q takes flags %v, and the original %v, so a setting would read differently",
				name, flagDefaults(ported.analyzer), flagDefaults(original))
		}
	}
}

// flagDefaults is each flag analyzer takes, by name, with its default, so a port that drops, adds or defaults a flag
// differently from its original, printf's -funcs for one, reads as different.
func flagDefaults(analyzer *analysis.Analyzer) map[string]string {
	defaults := map[string]string{}
	analyzer.Flags.VisitAll(func(defined *flag.Flag) {
		defaults[defined.Name] = defined.DefValue
	})
	return defaults
}

// TestFlagDefaultsTellFlagSetsApart holds flagDefaults to telling analyzers apart by their flags: a flag one takes and
// the other lacks, and the same flag with another default, each differ, and the same flag with the same default does
// not, so the port check above cannot pass by reading every analyzer's flags as the same.
func TestFlagDefaultsTellFlagSetsApart(test *testing.T) {
	test.Parallel()
	withFlag := func(defaultValue string) *analysis.Analyzer {
		planted := &analysis.Analyzer{Name: "planted"}
		planted.Flags.String("funcs", defaultValue, "planted takes a flag")
		return planted
	}
	without := &analysis.Analyzer{Name: "planted"}
	if maps.Equal(flagDefaults(withFlag("")), flagDefaults(without)) {
		test.Error("an analyzer with a flag reads the same as one without it")
	}
	if maps.Equal(flagDefaults(withFlag("Warn")), flagDefaults(withFlag(""))) {
		test.Error("the same flag with another default reads the same")
	}
	if !maps.Equal(flagDefaults(withFlag("Warn")), flagDefaults(withFlag("Warn"))) {
		test.Error("the same flag with the same default reads as different")
	}
}

// TestAPortRunsInItsOriginalsPlace plants a port for bools and one for SA4006 and holds portOrOriginal to them: each
// planted port in its original's place, and every other check the original, so the table cannot fall silent and
// leave every check borrowed.
func TestAPortRunsInItsOriginalsPlace(test *testing.T) {
	test.Parallel()
	planted := map[string]port{}
	for _, name := range []string{"bools", "SA4006"} {
		planted[name] = port{analyzer: &analysis.Analyzer{
			Name: name,
			Doc:  "planted finds nothing; it stands in for a port of " + name,
			Run: func(pass *analysis.Pass) (any, error) {
				return nil, nil
			},
		}}
	}
	replaced := 0
	for name, original := range borrowedChecks() {
		running := portOrOriginal(original, planted).analyzer
		plantedPort, isPlanted := planted[name]
		switch {
		case isPlanted && running != plantedPort.analyzer:
			test.Errorf("%s runs its original, expected the planted port", name)
		case !isPlanted && running != original:
			test.Errorf("%s runs a planted port, expected its original", name)
		case isPlanted:
			replaced++
		}
	}
	if replaced != len(planted) {
		test.Errorf("the planted ports ran in %d places, expected %d", replaced, len(planted))
	}
}

// TestNilnessAndUnusedwriteFollowVetsSuite holds the two x/tools passes vet leaves out to the registry,
// after vet's suite, as tools rules under the originals' names, each running our port onto the engine's own
// intermediate representation through its dispatcher.
func TestNilnessAndUnusedwriteFollowVetsSuite(test *testing.T) {
	test.Parallel()
	expected := []struct {
		name     string
		listener *rule.Listener
	}{{"nilness", &nil_ness.Listener}, {"unusedwrite", &unused_write.Listener}}
	analyzers := []*analysis.Analyzer{}
	for _, port := range expected {
		registeredRule, found := registeredRuleNamed(rule.Namespace + port.name)
		if !found {
			test.Fatalf("%s is not registered", port.name)
		}
		if registeredRule.Listener != port.listener {
			test.Errorf("%q does not register its port's listener", port.name)
		}
		if !registeredRule.TypeAware {
			test.Errorf("%q reads a program form built from type information, so it is type aware", port.name)
		}
		analyzers = append(analyzers, registeredRule.Analyzer)
	}
	expectBlock(test, len(vet.Suite), analyzers, rule.OriginTools)
}

// TestEveryDispatchedRuleRequiresTheDispatcherHoldingItsListener holds every rule with a listener, over two calls to
// All as the engine, the docs and the tests make them, to the one shared dispatcher of its walk: its analyzer requires
// that dispatcher's, which holds its listener, every rule of a walk requires the same one, and both calls hand out
// the same analyzer, the one the settings set options on.
func TestEveryDispatchedRuleRequiresTheDispatcherHoldingItsListener(test *testing.T) {
	test.Parallel()
	first, second := All(), All()
	analyzerOf := map[string]*analysis.Analyzer{}
	dispatcherOf := map[string]*analysis.Analyzer{}
	for _, registered := range slices.Concat(first, second) {
		if registered.Listener == nil {
			continue
		}
		shared, holds := dispatch.Shared(registered.Listener)
		if !holds {
			test.Errorf("the shared dispatcher of %s's walk does not hold its listener, so its analyzer would read "+
				"no outcome", registered.Name)
		}
		if !slices.Contains(registered.Analyzer.Requires, shared) {
			test.Errorf("%s does not require the shared dispatcher of its walk, so another dispatcher would walk for "+
				"it", registered.Name)
		}
		walk := listenerWalk(registered.Listener)
		if earlier, seen := dispatcherOf[walk]; seen && earlier != shared {
			test.Errorf("%s's walk is shared by two dispatchers", registered.Name)
		}
		dispatcherOf[walk] = shared
		if earlier, seen := analyzerOf[registered.Name]; seen && earlier != registered.Analyzer {
			test.Errorf("two calls to All hand out two analyzers for %s, so options set on one miss the other",
				registered.Name)
		}
		analyzerOf[registered.Name] = registered.Analyzer
	}
	if len(dispatcherOf) != 2 {
		test.Errorf("the registry's dispatched rules hear %d walks, where the engine's own intermediate "+
			"representation's and the syntax's were expected", len(dispatcherOf))
	}
}

// listenerWalk names the walk listener hears, for a test that keys dispatchers by it: the engine's own intermediate
// representation and the syntax are two walks, each with its own shared dispatcher.
func listenerWalk(listener *rule.Listener) string {
	if listener.IsSyntax() {
		return "syntax"
	}
	return "the engine's own intermediate representation"
}

// heldTwice is a test-only rule whose listener is registered on the shared dispatchers of both walks, as a listener
// changed after it registered would leave it: on IntermediateRepresentation while it declared only that walk, then on
// Syntax once it declared the syntax's too. It is registered as the package initializes, before any test reads the
// shared dispatchers, and its callbacks do nothing, so a dispatcher that ever started it would find nothing.
var heldTwice = func() rule.Rule {
	listener := &rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{
		Function: func(*rule.Context, *intermediate_representation.Function) {
			// Never judges anything: the listener exists to be held by two dispatchers.
		},
	}}
	analyzer := dispatch.RuleAnalyzer(rule.Namespace+"test-held-twice-own", "test_held_twice_own",
		"a test-only rule on the engine's own walk", listener)
	listener.FileEnter = func(*rule.Context, *ast.File) {
		// Never judges anything: the listener exists to be held by two dispatchers.
	}
	dispatch.RuleAnalyzer(rule.Namespace+"test-held-twice-syntax", "test_held_twice_syntax",
		"the same test-only rule on the syntax walk", listener)
	return rule.Rule{Name: rule.Namespace + "test-held-twice", Origin: rule.OriginHouse, Analyzer: analyzer,
		Listener: listener}
}()

// TestEveryDispatchedRuleIsHeldByOneDispatcher holds every rule with a listener to exactly one shared dispatcher, the
// one its walk goes to: one left registered on both would be walked twice, while its analyzer read one outcome and
// the other went unread. The check also runs over heldTwice, a test-only rule whose
// listener is on both, to prove it can fail.
func TestEveryDispatchedRuleIsHeldByOneDispatcher(test *testing.T) {
	test.Parallel()
	for _, problem := range heldByOneProblems(registry) {
		test.Error(problem)
	}
	planted := heldByOneProblems([]rule.Rule{heldTwice})
	if len(planted) != 1 ||
		!strings.Contains(planted[0], `held by ["dispatch_intermediate_representation" "dispatch_syntax"]`) {
		test.Errorf("the rule held by both walks' dispatchers read as %q, where one problem naming both was expected",
			planted)
	}
}

// heldByOneProblems is, for each of registered with a listener not held by exactly the shared dispatcher of its walk,
// what holds it.
func heldByOneProblems(registered []rule.Rule) []string {
	problems := []string{}
	for _, held := range registered {
		if held.Listener == nil {
			continue
		}
		holders := dispatch.HeldBy(held.Listener)
		shared, _ := dispatch.Shared(held.Listener)
		if len(holders) == 1 && holders[0] == shared {
			continue
		}
		names := []string{}
		for _, holder := range holders {
			names = append(names, holder.Name)
		}
		problems = append(problems, fmt.Sprintf("%s's listener is held by %q, where only %s, the dispatcher of its "+
			"walk, was expected", held.Name, names, shared.Name))
	}
	return problems
}

// TestEveryRuleRequiringADispatcherCarriesItsListener holds every registered rule whose analyzer reads a dispatcher's
// outcome to carrying its listener, a port from the ports table as much as a house rule, so rule.Validate checks the
// listener and the test above holds the rule to the dispatcher that walks for it. A rule that required a dispatcher
// with no listener would run, reading its outcome from a dispatcher no check ties it to, so a port whose listener
// was left out of the table would pass every other test.
func TestEveryRuleRequiringADispatcherCarriesItsListener(test *testing.T) {
	test.Parallel()
	dispatched := 0
	for _, registered := range All() {
		if !slices.ContainsFunc(registered.Analyzer.Requires, dispatch.IsDispatcher) {
			continue
		}
		dispatched++
		if registered.Listener == nil {
			test.Errorf("%s's analyzer requires a dispatcher, and the rule carries no listener", registered.Name)
		}
	}
	if dispatched == 0 {
		test.Fatal("no registered rule requires a dispatcher, so nothing here was checked")
	}
}

// TestEverythingTheSyntaxDispatcherRequiresRunsDespiteErrors holds the syntax dispatcher's own walk to running despite
// errors, and every listener's requirement that does not to its rule's own analyzer. The syntax dispatcher runs
// despite errors, so on a package with errors each syntax rule is skipped for the errors, as it was when it walked on
// its own. That holds while everything the walk itself reads, transitively, runs despite errors too: a requirement of
// the walk that did not would fail the dispatcher, and every syntax rule with it, on every package with errors. A
// dispatcher made here, with no listener registered, requires exactly what the shared one's walk requires.
//
// A listener's own requirement is not held to running despite errors: the dispatcher runs past one skipped or failed
// on a package (rule.Failed), starting every other listener there. It is held instead to being required by its rule's
// own analyzer, on either dispatcher, so the checker fails the rule's action on that package with the sentence the
// rule gives walking by itself, "failed prerequisites" naming the requirement, as staticcheck's SA1019 did requiring
// its deprecated facts. check's TestAListenersRequirementSkippedForErrorsFailsOnlyItsOwnRule holds that sentence.
func TestEverythingTheSyntaxDispatcherRequiresRunsDespiteErrors(test *testing.T) {
	test.Parallel()
	visited := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer)
	visit = func(analyzer *analysis.Analyzer) {
		if visited[analyzer] {
			return
		}
		visited[analyzer] = true
		if !analyzer.RunDespiteErrors {
			test.Errorf("the syntax dispatcher's walk requires %s, which does not run despite errors", analyzer.Name)
		}
		for _, required := range analyzer.Requires {
			visit(required)
		}
	}
	visit(dispatch.NewSyntax().Analyzer())
	if len(visited) < 3 {
		test.Fatalf("the walk's requirements reached %d analyzers, where the dispatcher, inspect and staticcheck's "+
			"generated pass at least were expected", len(visited))
	}
	syntaxListeners := 0
	for _, registered := range All() {
		if registered.Listener == nil {
			continue
		}
		if registered.Listener.IsSyntax() {
			syntaxListeners++
			if _, held := dispatch.Shared(registered.Listener); !held {
				test.Fatalf("rule %s's listener is not held by the shared syntax dispatcher", registered.Name)
			}
		}
		for _, required := range registered.Listener.Requires {
			if runsDespiteErrorsThroughout(required) || slices.Contains(registered.Analyzer.Requires, required) {
				continue
			}
			test.Errorf("rule %s's listener requires %s, which does not run despite errors, and the rule's own "+
				"analyzer does not require it", registered.Name, required.Name)
		}
	}
	if syntaxListeners == 0 {
		test.Fatal("no registered rule listens to the syntax, so nothing here was checked")
	}
}

// runsDespiteErrorsThroughout is whether analyzer and everything it requires, transitively, run despite errors, so
// it finishes on a package with errors.
func runsDespiteErrorsThroughout(analyzer *analysis.Analyzer) bool {
	if !analyzer.RunDespiteErrors {
		return false
	}
	for _, required := range analyzer.Requires {
		if !runsDespiteErrorsThroughout(required) {
			return false
		}
	}
	return true
}

// TestNoRuleRequiresXToolsSingleAssignmentForm holds the run to one program form: no registered rule, nor
// anything it requires, needs x/tools' buildssa, which cost 1.2 to 1.7 CPU-s a cold run on cohere for two passes
// whose ports read the form staticcheck's checks already build (#pn1xvnd).
func TestNoRuleRequiresXToolsSingleAssignmentForm(test *testing.T) {
	test.Parallel()
	seen := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer, ruleName string)
	visit = func(analyzer *analysis.Analyzer, ruleName string) {
		if seen[analyzer] {
			return
		}
		seen[analyzer] = true
		if analyzer == buildssa.Analyzer {
			test.Errorf("%s requires x/tools' buildssa, so the run builds a second program form", ruleName)
		}
		for _, required := range analyzer.Requires {
			visit(required, ruleName)
		}
	}
	for _, registeredRule := range registry {
		visit(registeredRule.Analyzer, registeredRule.Name)
	}
	if len(seen) <= len(registry) {
		test.Errorf("the walk saw %d analyzers over %d rules, so it never reached what they require", len(seen),
			len(registry))
	}
}

// TestNoRuleRequiresStaticchecksIntermediateRepresentation holds the run to the engine's own intermediate
// representation alone (#kk4864m): no registered rule, nor anything it requires, needs staticcheck's buildir, which
// built a second program form beside ours on every package. The check also runs over planted rules, to prove it can
// fail and that it blames the rule that brings buildir in, not every rule sharing its dispatcher.
func TestNoRuleRequiresStaticchecksIntermediateRepresentation(test *testing.T) {
	test.Parallel()
	for _, problem := range buildirProblems(registry) {
		test.Error(problem)
	}

	run := func(pass *analysis.Pass) (any, error) {
		return nil, nil
	}
	plantedBuildir := &analysis.Analyzer{Name: "buildir", Doc: "planted stands in for staticcheck's buildir", Run: run}
	plantedRead := &analysis.Analyzer{Name: "planted_read", Doc: "planted reads buildir's result", Run: run,
		Requires: []*analysis.Analyzer{plantedBuildir}}
	plantedRequiring := &analysis.Analyzer{Name: "planted_requiring", Doc: "planted requires buildir", Run: run,
		Requires: []*analysis.Analyzer{plantedRead}}
	plantedClean := &analysis.Analyzer{Name: "planted_clean", Doc: "planted requires nothing", Run: run}
	// Two syntax rules on a dispatcher of their own: the first's listener requires buildir, which the dispatcher then
	// requires too, and the second's does not, so only the first is to blame.
	syntax := dispatch.NewSyntax()
	readingListener := &rule.Listener{FileEnter: plantedFileEnter, Requires: []*analysis.Analyzer{plantedRead}}
	cleanListener := &rule.Listener{FileEnter: plantedFileEnter}
	planted := buildirProblems([]rule.Rule{
		{Name: "cohere-go/planted-requiring", Analyzer: plantedRequiring},
		{Name: "cohere-go/planted-reading", Listener: readingListener, Analyzer: syntax.RuleAnalyzer(
			"cohere-go/planted-reading", "planted_reading", "planted listener reads buildir", readingListener)},
		{Name: "cohere-go/planted-sharing", Listener: cleanListener, Analyzer: syntax.RuleAnalyzer(
			"cohere-go/planted-sharing", "planted_sharing", "planted listener shares the dispatcher", cleanListener)},
		{Name: "cohere-go/planted-clean", Analyzer: plantedClean},
	})
	expected := []string{
		"cohere-go/planted-requiring requires buildir through planted_requiring -> planted_read -> buildir",
		"cohere-go/planted-reading requires buildir through planted_reading -> planted_read -> buildir",
	}
	if len(planted) != len(expected) {
		test.Fatalf("the planted rules have %d problems, expected %d: %q", len(planted), len(expected), planted)
	}
	for index, saying := range expected {
		if !strings.Contains(planted[index], saying) {
			test.Errorf("planted problem %d is %q, expected it to say %q", index, planted[index], saying)
		}
	}
}

// buildirProblems is, for each of registered that requires staticcheck's buildir, the path its requirements take to
// it.
func buildirProblems(registered []rule.Rule) []string {
	problems := []string{}
	for _, checked := range registered {
		if path := pathToBuildir(checked.Analyzer); path != nil {
			problems = append(problems, fmt.Sprintf("%s requires buildir through %s", checked.Name,
				strings.Join(path, " -> ")))
		}
	}
	return problems
}

// pathToBuildir is the names of the analyzers from start to staticcheck's buildir along its requirements, both ends
// included, or nil when none reaches it. Of a dispatcher's requirements it follows only its walk's own: what a
// listener requires, the dispatcher requires after them (dispatch.ListenerRequirement), and the listener's rule's own
// analyzer requires it too, so the path that blames the rule is the one through its own analyzer, and the checker runs
// it only where that rule runs.
func pathToBuildir(start *analysis.Analyzer) []string {
	visited := map[*analysis.Analyzer]bool{}
	var walk func(analyzer *analysis.Analyzer) []string
	walk = func(analyzer *analysis.Analyzer) []string {
		if visited[analyzer] {
			return nil
		}
		visited[analyzer] = true
		// By name, the name staticcheck gives it, since its package is internal to staticcheck's module and cohere
		// cannot import it to compare the analyzer itself.
		if analyzer.Name == "buildir" {
			return []string{analyzer.Name}
		}
		for _, required := range analyzer.Requires {
			if dispatch.ListenerRequirement(analyzer, required) {
				continue
			}
			if rest := walk(required); rest != nil {
				return append([]string{analyzer.Name}, rest...)
			}
		}
		return nil
	}
	return walk(start)
}

// TestStaticcheckFollowsTheToolsPasses holds the registry, after the x/tools passes, to every default SA
// and S check, then the ruled ST checks, then U1000 as our port, then the house rules, and nothing else, which
// also keeps out every QF check, since a quick fix is not a finding.
func TestStaticcheckFollowsTheToolsPasses(test *testing.T) {
	test.Parallel()
	offset := len(vet.Suite) + 2
	checks := staticcheckAnalyzers()
	expectBlock(test, offset, checks, rule.OriginStaticcheck)
	styleChecks := stylecheckAnalyzers()
	expectBlock(test, offset+len(checks), styleChecks, rule.OriginStaticcheck)

	unusedIndex := offset + len(checks) + len(styleChecks)
	expectedCount := unusedIndex + 1 + len(House())
	if len(registry) != expectedCount {
		test.Fatalf("the registry has %d rules, expected %d", len(registry), expectedCount)
	}
	expectedUnused := rule.Rule{
		Name:         "cohere-go/U1000",
		Origin:       rule.OriginStaticcheck,
		UpstreamName: "U1000",
		TypeAware:    true,
		Analyzer:     unused_code.Analyzer,
		MergeIfAll:   true,
	}
	// reflect.DeepEqual because ConfiguredBy is a slice, which leaves rule.Rule without ==.
	if unusedRule := registry[unusedIndex]; !reflect.DeepEqual(unusedRule, expectedUnused) {
		test.Errorf("rule %d is %+v, expected %+v", unusedIndex, unusedRule, expectedUnused)
	}
	for index, houseRule := range House() {
		registeredRule := registry[unusedIndex+1+index]
		if registeredRule.Name != houseRule.Name || registeredRule.Origin != rule.OriginHouse {
			test.Errorf("rule %d is %q (%s), expected the house rule %q", unusedIndex+1+index, registeredRule.Name,
				registeredRule.Origin, houseRule.Name)
		}
	}
}

// TestU1000RunsThePort holds the U1000 rule to cohere's port, under the original's name, so its settings, its docs
// and every //cohere:ignore naming it read as they did when it ran staticcheck's original, which is now the oracle
// the port's own tests hold it to.
func TestU1000RunsThePort(test *testing.T) {
	test.Parallel()
	registeredRule, found := registeredRuleNamed("cohere-go/U1000")
	if !found {
		test.Fatal("the registry holds no cohere-go/U1000")
	}
	if registeredRule.Analyzer != unused_code.Analyzer {
		test.Errorf("cohere-go/U1000 runs another analyzer named %q, not unused_code.Analyzer, cohere's port",
			registeredRule.Analyzer.Name)
	}
	if registeredRule.UpstreamName != "U1000" || registeredRule.Origin != rule.OriginStaticcheck ||
		!registeredRule.TypeAware {
		test.Errorf("cohere-go/U1000 is %+v, expected upstream U1000, origin staticcheck and type aware",
			registeredRule)
	}
}

// TestEveryPortedStylecheckRunsItsPort holds each switched ST check to its port: the registered rule under the
// check's name runs the port's own analyzer, and not stylecheck's original, which the switch replaced.
func TestEveryPortedStylecheckRunsItsPort(test *testing.T) {
	test.Parallel()
	originals := map[string]*analysis.Analyzer{}
	for _, wrapped := range stylecheck.Analyzers {
		originals[wrapped.Analyzer.Name] = wrapped.Analyzer
	}
	for _, ported := range portedStylecheck {
		registeredRule, found := registeredRuleNamed(rule.Namespace + ported.name)
		if !found {
			test.Errorf("%s%s is not registered", rule.Namespace, ported.name)
			continue
		}
		original, named := originals[ported.name]
		switch {
		case !named:
			test.Errorf("stylecheck names no check %s, so there is no original for its port to replace", ported.name)
		case registeredRule.Analyzer == original:
			test.Errorf("%s runs stylecheck's original, where its port was switched in", registeredRule.Name)
		case registeredRule.Analyzer != ported.port:
			test.Errorf("%s runs %q, where its port's own analyzer was expected", registeredRule.Name,
				registeredRule.Analyzer.Name)
		}
	}
}

// mergedIfAll is every registered rule whose finding stands, where builds differ, only if every build with a say on
// its file reports it, written out here rather than read from staticcheck's documentation, so a strategy the registry
// drops, or a port that stops merging as its original does, fails TestExactlyTheMergeIfAllChecksMergeIfAll. They are
// the registered checks whose documentation says MergeIfAll, and U1000, which staticcheck's runner merges that way by
// hand. One check a line.
var mergedIfAll = []string{
	"S1001",
	"S1002",
	"S1011",
	"S1016",
	"S1019",
	"S1025",
	"S1031",
	"S1039",
	"S1040",
	"SA4003",
	"SA4004",
	"SA4006",
	"SA4008",
	"SA4010",
	"SA4014",
	"SA4015",
	"SA4017",
	"SA4020",
	"SA9005",
	"SA9010",
	"U1000",
}

// TestExactlyTheMergeIfAllChecksMergeIfAll holds the registry's merge strategies to staticcheck's, both ways: every
// check mergedIfAll names merges if all, a port as its original, and every other rule merges if any.
func TestExactlyTheMergeIfAllChecksMergeIfAll(test *testing.T) {
	test.Parallel()
	var merged []string
	for _, registered := range registry {
		if registered.MergeIfAll {
			merged = append(merged, strings.TrimPrefix(registered.Name, rule.Namespace))
		}
	}
	slices.Sort(merged)
	if !slices.Equal(merged, mergedIfAll) {
		test.Errorf("the rules that merge if all are %v, where %v were expected", merged, mergedIfAll)
	}
}

// TestEveryPortedStaticcheckRunsItsPort holds each switched SA or S check to its port: the registered rule under the
// check's name runs the port's own analyzer, and not staticcheck's original, which the switch replaced.
func TestEveryPortedStaticcheckRunsItsPort(test *testing.T) {
	test.Parallel()
	originals := map[string]*analysis.Analyzer{}
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers) {
		originals[wrapped.Analyzer.Name] = wrapped.Analyzer
	}
	for _, ported := range portedStaticcheck {
		registeredRule, found := registeredRuleNamed(rule.Namespace + ported.name)
		if !found {
			test.Errorf("%s%s is not registered", rule.Namespace, ported.name)
			continue
		}
		original, named := originals[ported.name]
		switch {
		case !named:
			test.Errorf("staticcheck names no check %s, so there is no original for its port to replace", ported.name)
		case registeredRule.Analyzer == original:
			test.Errorf("%s runs staticcheck's original, where its port was switched in", registeredRule.Name)
		case registeredRule.Analyzer != ported.port:
			test.Errorf("%s runs %q, where its port's own analyzer was expected", registeredRule.Name,
				registeredRule.Analyzer.Name)
		}
	}
}

// TestExactlyTheRuledStylecheckChecksAreRegistered holds the ST rules to the ruling, both ways: every ruled
// check is registered, and no other ST check is, the ruled-off ST1003 and ST1005 above all.
func TestExactlyTheRuledStylecheckChecksAreRegistered(test *testing.T) {
	test.Parallel()
	registeredStylecheck := []string{}
	for _, registeredRule := range registry {
		if strings.HasPrefix(registeredRule.UpstreamName, "ST") {
			registeredStylecheck = append(registeredStylecheck, registeredRule.UpstreamName)
		}
	}
	slices.Sort(registeredStylecheck)
	if !slices.Equal(registeredStylecheck, ruledStylecheck) {
		test.Errorf("the registered ST checks are %q, expected exactly %q", registeredStylecheck, ruledStylecheck)
	}
}

// TestNoDefaultOffSaOrSCheckIsRegistered holds the registry to staticcheck's own defaults for SA and S: a
// check its documentation marks NonDefault stays off until it is ruled on. SA9003 is named, so the test
// cannot pass by staticcheck marking nothing.
func TestNoDefaultOffSaOrSCheckIsRegistered(test *testing.T) {
	test.Parallel()
	nonDefault := []string{}
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers) {
		if wrapped.Doc != nil && wrapped.Doc.NonDefault {
			nonDefault = append(nonDefault, wrapped.Analyzer.Name)
		}
	}
	if !slices.Contains(nonDefault, "SA9003") {
		test.Errorf("staticcheck's default-off SA and S checks are %q, which no longer holds SA9003", nonDefault)
	}
	for _, code := range nonDefault {
		if _, found := registeredRuleNamed(rule.Namespace + code); found {
			test.Errorf("%s is registered, and staticcheck leaves it off by default", code)
		}
	}
}

// TestTheRegisteredAnalyzersValidateTogether runs x/tools' own check over the whole set, which refuses a
// cycle in the Requires graph and a fact type an analyzer uses without declaring, either of which would
// fail the engine's run rather than one rule.
func TestTheRegisteredAnalyzersValidateTogether(test *testing.T) {
	test.Parallel()
	analyzers := []*analysis.Analyzer{}
	for _, registeredRule := range registry {
		analyzers = append(analyzers, registeredRule.Analyzer)
	}
	if validateError := analysis.Validate(analyzers); validateError != nil {
		test.Error(validateError)
	}
}

// TestTypeAwareFollowsTheSyntaxOnlyTable proves the table is read the right way round, with one pass
// from each side of it, and that no entry names a pass the registry does not hold, which would mean a
// rename upstream left a decision about nothing.
func TestTypeAwareFollowsTheSyntaxOnlyTable(test *testing.T) {
	test.Parallel()
	expected := map[string]bool{
		"cohere-go/printf":   true,
		"cohere-go/buildtag": false,
		"cohere-go/SA4006":   true,
		"cohere-go/SA9009":   false,
		"cohere-go/ST1008":   true,
		"cohere-go/ST1018":   false,
	}
	for name, typeAware := range expected {
		registeredRule, found := registeredRuleNamed(name)
		if !found {
			test.Errorf("%s is not registered", name)
			continue
		}
		if registeredRule.TypeAware != typeAware {
			test.Errorf("%s has TypeAware %t, expected %t", name, registeredRule.TypeAware, typeAware)
		}
	}
	for upstreamName := range syntaxOnly {
		if _, found := registeredRuleNamed(rule.Namespace + upstreamName); !found {
			test.Errorf("the syntax-only table names %q, which is not a registered rule", upstreamName)
		}
	}
}

// TestNoCheckDuplicatesAVetPass holds the registry to one rule per judgment: a staticcheck check that judges
// what a vet pass judges is left out while that pass is registered. SA5009 is named, so the test cannot pass
// by the table emptying.
func TestNoCheckDuplicatesAVetPass(test *testing.T) {
	test.Parallel()
	if duplicatesOfVet["SA5009"] != "printf" {
		test.Errorf("SA5009 is no longer recorded as printf's twin: %q", duplicatesOfVet)
	}
	for code, vetPass := range duplicatesOfVet {
		if _, found := registeredRuleNamed(rule.Namespace + vetPass); !found {
			test.Errorf("%s is left out as %s's twin, and %s is not registered", code, vetPass, vetPass)
		}
		if _, found := registeredRuleNamed(rule.Namespace + code); found {
			test.Errorf("%s is registered beside %s, so one mistake would be two findings", code, vetPass)
		}
	}
}

// sharedReaders is the packages under rules/ that several rules read through and that hold no rule of their own: marker
// reads the reasoned marker comments, inert_value judges a value that cannot carry syntax, marker_type judges a sum
// type's variant whose methods only mark it, served finds the types a file serves for the two rules that count them,
// positions is the line table rules read a node's position from without the FileSet's lock, function_purity judges
// which functions are pure for the rules asking which calls have side effects, return_nil_ness judges whether each
// function's results may be nil for the rule asking what a call returns, build_intermediate_representation builds the
// engine's own intermediate representation for the rules that read it, and the four kits the ports of staticcheck's
// checks share (#d8s4rsy) are staticcheck_report, where they report, staticcheck_code, the helpers they call,
// staticcheck_call, the walk of call arguments, and staticcheck_wants, the reader of staticcheck's own fixtures, and
// dispatch walks a package once for every rule with a listener and builds each such rule's analyzer, holding no rule
// itself, and child_process names the pipe, command and wait the two rules about a child's piped output share.
// One name a line, in a slice rather than a map, since gofmt aligns a map's values to its longest key and would rewrite
// every line when a branch adds a longer one; a line of a slice is never rewritten for another's sake.
var sharedReaders = []string{
	"build_intermediate_representation",
	"child_process",
	"dispatch",
	"function_purity",
	"inert_value",
	"marker",
	"marker_type",
	"positions",
	"return_nil_ness",
	"served",
	"staticcheck_call",
	"staticcheck_code",
	"staticcheck_report",
	"staticcheck_wants",
}

// portedPasses is the packages under rules/ that hold an upstream pass ported as cohere's own: onto staticcheck's
// intermediate representation, over the syntax, or, for vet, one package for each of vet's passes (#z2x1c7g). Each
// runs beside its original until it matches it finding for finding, and then All registers it under the original's
// name in the original's place (#pn1xvnd), so House never lists it. One entry a line, and this comment names none, so
// each port's branch adds its own line and the ports land one after another without touching each other's lines.
var portedPasses = []string{
	"address_compared_to_nil",
	"argument_overwritten_before_use",
	"assertion_to_own_type",
	"assigned_benchmark_iterations",
	"boolean_constant_comparison",
	"boolean_return_of_condition",
	"buffer_string_conversion",
	"byte_slice_write_string",
	"bytes_compare_equality",
	"case_insensitive_comparison",
	"constant_group_type",
	"cyclic_finalizer",
	"defer_in_channel_range_loop",
	"defer_in_infinite_loop",
	"deferred_lock_after_lock",
	"deprecated_use",
	"discarded_pure_call",
	"dot_import",
	"double_negation",
	"duplicate_build_constraint",
	"duplicate_import",
	"duplicate_trim_character",
	"duration_unit_suffix",
	"dynamic_printf_format",
	"early_deferred_close",
	"elaborate_sleep",
	"empty_critical_section",
	"empty_select_default",
	"error_variable_name",
	"errors_new_of_sprintf",
	"extreme_comparison",
	"failed_type_assertion_read",
	"find_all_zero_limit",
	"guarded_delete",
	"guarded_map_access",
	"hoisted_map_key_conversion",
	"http_status_literal",
	"identical_operands",
	"impossible_target_comparison",
	"impossible_type_assertion",
	"inappropriate_context_key",
	"inconsistent_receiver_name",
	"index_compared_as_contains",
	"ineffective_bitwise_operation",
	"ineffective_break",
	"ineffective_copy",
	"ineffective_field_assignment",
	"ineffective_random_integer",
	"ineffective_sort",
	"ineffective_url_query_modification",
	"infinite_loop_condition",
	"infinite_recursion",
	"integer_division_to_zero",
	"invalid_listen_address",
	"invalid_regular_expression",
	"invalid_string_conversion_argument",
	"invalid_struct_tag",
	"invalid_template",
	"invalid_time_layout",
	"invalid_url",
	"invalid_utf8_cut_set",
	"invisible_character",
	"ip_compared_as_bytes",
	"leaky_time_tick",
	"loop_copy",
	"manual_trim",
	"marshaled_struct_without_exported_fields",
	"math_on_converted_integer",
	"misaligned_atomic_field",
	"misplaced_error_result",
	"modified_writer_buffer",
	"modulo_by_one",
	"nan_comparison",
	"nanosecond_sleep",
	"negative_length_comparison",
	"negative_zero_float",
	"never_nil_comparison",
	"nil_context_argument",
	"nil_map_assignment",
	"nil_ness",
	"non_canonical_header_key",
	"non_octal_file_mode",
	"non_pointer_pool_value",
	"non_pointer_unmarshal",
	"odd_slice_argument",
	"overlapping_encode_buffers",
	"oversized_shift",
	"print_of_sprintf",
	"range_over_rune_conversion",
	"range_over_string_directly",
	"receiver_name",
	"redundant_assertion_nil_check",
	"redundant_canonical_header_key",
	"redundant_control_flow",
	"redundant_make_arguments",
	"redundant_nil_check_around_loop",
	"redundant_nil_check_on_length",
	"redundant_slice_length",
	"redundant_sprintf_of_string",
	"regular_expression_match_in_loop",
	"regular_expression_raw_string",
	"removed_user_directory",
	"repeated_if_else_condition",
	"replace_zero_count",
	"reversed_errors_is_arguments",
	"self_assignment",
	"separate_declaration_and_assignment",
	"shell_command_as_program",
	"single_append_concatenation",
	"single_argument_append",
	"single_case_select",
	"sliding_copy",
	"sort_helper_call",
	"sort_slice_on_non_slice",
	"spaced_compiler_directive",
	"spinning_empty_loop",
	"struct_conversion_by_field_copy",
	"swapped_seek_arguments",
	"test_failure_in_goroutine",
	"time_since",
	"time_until",
	"timer_reset_result_used",
	"type_switch_bound_value",
	"typed_nil_interface_comparison",
	"unbuffered_signal_channel",
	"uncalled_deferred_function",
	"unchanging_loop_condition",
	"unconditional_loop_exit",
	"unencodable_binary_value",
	"unexiting_test_main",
	"unnecessary_blank_identifier",
	"unnecessary_sprint",
	"unobserved_append",
	"unreachable_type_case",
	"unread_assigned_value",
	"unsupported_marshal_type",
	"untrappable_signal",
	"unused_code",
	"unused_write",
	"vet",
	"wait_group_add_in_goroutine",
	"yoda_condition",
}

// TestEveryRulePackageIsInHouse holds the registry to the rule packages on disk: each directory under rules/, but
// testdata, the shared readers and the ported passes, is one house rule named for it, and House lists it. A rule
// package nobody registered would build, pass its own fixtures and run on nothing.
func TestEveryRulePackageIsInHouse(test *testing.T) {
	test.Parallel()
	entries, readError := os.ReadDir(".")
	if readError != nil {
		test.Fatalf("reading the rules directory: %v", readError)
	}
	registeredHouse := map[string]bool{}
	for _, houseRule := range House() {
		registeredHouse[houseRule.Name] = true
	}
	packages := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "testdata" || slices.Contains(sharedReaders, entry.Name()) ||
			slices.Contains(portedPasses, entry.Name()) {
			continue
		}
		packages++
		name := rule.Namespace + strings.ReplaceAll(entry.Name(), "_", "-")
		if !registeredHouse[name] {
			test.Errorf("rules/%s holds a rule, and House does not list %s", entry.Name(), name)
		}
	}
	if packages != len(House()) {
		test.Errorf("rules/ holds %d rule packages, and House lists %d rules", packages, len(House()))
	}
}

// ST1015 judges a misplaced default the way the house rule consistency-require-default-last does, so it stays out
// of the registry while that rule is in the house, and a misplaced default is one finding, not two.
func TestStylecheckLeavesTheDefaultClauseToTheHouseRule(test *testing.T) {
	test.Parallel()
	for _, registered := range All() {
		if registered.Name == rule.Namespace+"ST1015" {
			test.Fatalf("%s is registered beside cohere-go/consistency-require-default-last, which judges the same "+
				"default", registered.Name)
		}
	}
}
