# cold_arguments prints, one to a line, the arguments quiet_machine.sh's cold and idle runs give cohere, for the engine
# a run is narrowed to with --engine, or for the whole root when it is given none.
#
# Every cold run reads nothing from and writes nothing to the caches (`--no-cache`). One narrowed to the Go engine
# also checks without fixing (`--no-fix`): it is #pff7xhr's bar, which is a checking run's seconds, and a fixing run
# spends about 0.3s formatting before its load (#jp0cqkk). A whole-root run fixes as it always has, so its records
# still compare with the baselines taken that way. The arguments live here, apart from the benchmark, so a test runs
# them alone (internal/bench_results' cold_arguments_test.go) rather than through a measurement.
cold_arguments() {
  print -r -- --no-cache
  [[ -n $1 ]] && print -r -- --no-fix
  return 0
}
