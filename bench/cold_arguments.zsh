# cold_arguments prints, one to a line, the arguments quiet_machine.sh's cold and idle runs give cohere, for the engine
# a run is narrowed to with --engine, or for the whole root when it is given none.
#
# Every cold run reads nothing from and writes nothing to the caches (`--no-cache`). One narrowed to the Go engine
# also checks without fixing (`--no-fix`): it is #pff7xhr's bar, which is a checking run's seconds, and a fixing run
# spends about 0.3s formatting before its load (#jp0cqkk). A whole-root run fixes as it always has, so its records
# still compare with the baselines taken that way. The arguments live here, apart from the benchmark, so a test runs
# them alone (internal/bench_results' cold_arguments_test.go) rather than through a measurement.
# build_arguments prints, one to a line, the arguments every run gives cohere for the builds it checks: none for the
# host's build alone, as every record before #ccqee0e measured, and `--other-builds` for every build the Go files name.
# Every run takes them, primes, warm and edit runs too, so each mode measures the same run. When every build becomes a
# plain run's default (#qqyq61s), every prints none here and host prints the switch that asks for the host's alone.
build_arguments() {
  [[ ${1:-} == every ]] && print -r -- --other-builds
  return 0
}

# Both read their argument as ${1:-}: quiet_machine.sh runs under `set -u`, where reading an unset argument is an
# error, and a whole-root run names no engine: a bare $1 failed after `--no-cache` was printed, so the cold runs
# stayed cold only by the order of the two lines.
cold_arguments() {
  print -r -- --no-cache
  [[ -n ${1:-} ]] && print -r -- --no-fix
  return 0
}
