#!/bin/zsh
# The quiet-machine benchmark: how long `cohere` takes on a real project, cold, on an unchanged replay,
# and after a one-file edit, measured so that the number means what it says.
#
# It measures a copy of the project pinned to its commits, never the project itself, with one cohere
# engine for every run. pinned.zsh, which it shares with rivals.sh, says how and why.
#
# The command measured is `cohere --json`: the one a developer runs, so a warm run gets the caches a
# developer's run would, printing its versioned contract instead of the footer. The footer is for people and
# free to change, and when it became one line, everything this benchmark read from it read nothing
# (#zrgrk14), so each run's engine seconds, findings and cache use are read from its --json summary by
# internal/benchresults/tools/runoutput, which is tested against real output. That command may write, so
# the copy's files are hashed before the first run and after the last one, and a benchmark that changed a
# byte of them refuses to report.
#
# One round, interleaved, so a load that drifts lands on every mode alike:
#   prime   `cohere`, not measured, so the cache describes the tree as it stands: run again until one
#           replays, up to three times, since a first run after a new engine or the cold run can leave
#           a cache the next run does not replay whole (measured 2026-10-05: prime and warm both checked
#           all 3,976 files, and the run after replayed, #547dhjz). Each run's row carries how many
#           primes its round needed, and the summary names every round that needed more than one, so
#           the retry cannot hide that bug
#   warm    `cohere` again on the unchanged tree: the replay
#   edit    one line added to one function body, `cohere`, then the file's original bytes put back
#   cold    `cohere --no-cache`: every phase from source, nothing read or written
# The edit is a comment line, inserted above a `return` statement, so the file's bytes and every
# content hash cohere keys on change while its types and findings do not. A round whose edit run
# reports a different finding count from its warm run says so, because then the edit did more than that.
#
# Every run is printed with the one-minute load before and after it. A run is quiet only when both are
# at or under the ceiling, and a best or median is only called quiet when it is taken from quiet runs.
# A mode with no quiet runs gets no quiet number, only the loaded ones labelled as loaded, because a
# best-of-3 taken on a busy machine and one taken on a quiet one look the same afterwards.
#
# The ending load counts the run's own work, and that is deliberate. A two-second cold run on every core
# lifts the one-minute load by about one on its own, so a cold run that starts just under the ceiling can
# end over it and be labelled loaded: on 2026-10-03 two of five cold runs were excluded this way. Loosening
# the check would also stop it catching a build that starts mid-run. A strict check only costs samples
# and never flatters a number, so it stays strict; for cold, give it a machine that starts lower.
#
# Two times per run. `verdict` is what a developer waits for: the launcher returns when the engine has
# its exit code, and the engine writes its cache afterwards. `settled` is when every process the run
# started has exited, cache write included. The next run starts only after that. The engine runs
# directly with the verdict descriptor the launcher would have given it, so the early return is still
# what is timed.
#
# usage: quiet_machine.sh [options]
#   --project DIR      the project to copy (default ~/Projects/ahra)
#   --runs N           rounds, so N runs of each mode (default 5)
#   --load-ceiling L   the highest one-minute load a quiet run may start or end at (default: cores / 4)
#   --settle SECONDS   before each measured run, wait up to this long for the load to reach the ceiling
#                      (default 0, which does not wait)
#   --edit PATH        the file the edit run changes, relative to the project
#                      (default modules/tasks/TasksSearchQuery.ts)
#   --cohere PATH      the cohere launcher, or an engine binary, to run (default: `cohere` on PATH)
#   --work DIR         where the copies live (default $TMPDIR/cohere-quiet-machine)
#   --record           also write the run to bench/results as a record to commit
#                      (internal/benchresults): the machine, the commits, every run and each mode's
#                      numbers, refused when it disagrees with its own runs or the engine is dirty
#
# Exit status: 0 when every mode has a quiet number, 3 when some mode has none, 1 when it could not
# measure at all, including a copy that changed under it or a cohere that changed between runs.
set -u
name=quiet_machine
source ${0:A:h}/pinned.zsh

project=$HOME/Projects/ahra
runs=5
cores=$(sysctl -n hw.ncpu)
ceiling=$(awk -v cores=$cores 'BEGIN { print cores / 4 }')
settle=0
edit=modules/tasks/TasksSearchQuery.ts
cohere=cohere
work=${TMPDIR:-/tmp}/cohere-quiet-machine
record=false

while (( $# > 0 )); do
  case $1 in
    --project) project=$2; shift 2 ;;
    --runs) runs=$2; shift 2 ;;
    --load-ceiling) ceiling=$2; shift 2 ;;
    --settle) settle=$2; shift 2 ;;
    --edit) edit=$2; shift 2 ;;
    --cohere) cohere=$2; shift 2 ;;
    --work) work=$2; shift 2 ;;
    --record) record=true; shift ;;
    *) print -u2 "quiet_machine: unknown argument $1 (see the usage at the top of this script)"; exit 1 ;;
  esac
done
(( runs >= 1 )) || fail "--runs must be at least 1"

prepare_copy
[[ -f $copy/$edit ]] || fail "no file $edit in the copy to edit"

logs=$copy/.cache/quiet-machine-$(date +%Y%m%d-%H%M%S)
mkdir -p $logs
runs_table=$logs/runs.tsv
pin_engine $logs
run_output_reader=$logs/runoutput
(cd ${0:A:h:h} && go build -o $run_output_reader ./internal/benchresults/tools/runoutput) ||
  fail "could not build the run output reader"

print "# quiet-machine benchmark, $(date -u +%Y-%m-%dT%H:%M:%SZ)"
print_pinned_header
print "# rounds   $runs, load ceiling $ceiling (one-minute), edit $edit"
print

# run_cohere runs the pinned engine in the copy; see run_timed.
run_cohere() {
  local log=$1; shift
  run_timed $log $engine --json "$@"
}

measure() {
  local mode=$1 round=$2; shift 2
  wait_for_quiet
  local before=$(load)
  local log=$logs/$mode-$round.log
  run_cohere $log "$@"
  local after=$(load)
  local quiet=loaded
  under_ceiling $before && under_ceiling $after && quiet=quiet
  # The run's engine seconds, findings and cache use, from its --json summary. A log the reader cannot
  # read stops the benchmark rather than recording a column it could not fill.
  local output engine_seconds findings cache
  output=$($run_output_reader $log) || fail "could not read the run's output, see $log"
  IFS=$'\t' read -r engine_seconds findings cache <<< $output
  printf '%s\t%d\t%.3f\t%.3f\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\n' $mode $round $verdict_seconds $settled_seconds \
    $engine_seconds $findings "$cache" $before $after $quiet $exit_code $primes |
    tee -a $runs_table
}

edit_original=$logs/edit-original
cp $copy/$edit $edit_original
original_hash=$(shasum -a 256 < $edit_original)

printf 'mode\tround\tverdict_s\tsettled_s\tengine_s\tfindings\tcache\tload_before\tload_after\tquiet\texit\tprimes\n'
for round in $(seq 1 $runs); do
  primes=0
  for prime in 1 2 3; do
    primes=$prime
    run_cohere $logs/prime-$round-$prime.log
    [[ $($run_output_reader $logs/prime-$round-$prime.log | cut -f3) == replay ]] && break
  done

  measure warm $round

  # The comment goes above the first `return` that opens a statement of its own, so it lands in a
  # function body and cannot become the body of an unbraced `if`.
  awk -v marker="cohere quiet-machine edit, round $round" '
    !done && /^[ \t]+return[ ;(]/ && previous ~ /[{;}][ \t]*$/ {
      match($0, /^[ \t]+/); print substr($0, 1, RLENGTH) "// " marker; done = 1
    }
    { print; previous = $0 }
    END { if (!done) exit 1 }' $edit_original > $logs/edit-$round ||
    fail "found no return statement in $edit to put the edit above"
  cat $logs/edit-$round > $copy/$edit
  measure edit $round
  cat $edit_original > $copy/$edit
  [[ $(shasum -a 256 < $copy/$edit) == $original_hash ]] || fail "$edit did not go back to its original bytes"

  measure cold $round --no-cache
done

[[ $(tree_hash) == $expected_tree ]] ||
  fail "the copy's files changed during the benchmark, so the runs did not all see the same tree; see $logs"

# A warm and an edit run in the same round should report the same findings; when they do not, the edit
# changed more than bytes.
awk -F'\t' '$1 == "warm" { warm[$2] = $6 } $1 == "edit" { edit[$2] = $6 }
  END { for (round in edit) if (edit[round] != warm[round])
    printf "note: round %s edit reported %s findings and warm %s, so the edit was not neutral\n", round, edit[round], warm[round] }' $runs_table

# A round whose prime did not replay on its first run met #547dhjz: a run after a change that left a cache
# the next run could not replay whole. The retry keeps the warm run a real replay, and this says it was needed.
awk -F'\t' '$1 == "warm" && $12 > 1 {
    printf "note: round %s needed %s primes before a run replayed whole (#547dhjz); see its prime logs\n", $2, $12 }' $runs_table

print
print "verdict seconds, by mode (quiet runs are the number; loaded runs are shown, not reported):"
outcome=0
for mode in cold warm edit; do
  quiet_runs=($(awk -F'\t' -v mode=$mode '$1 == mode && $10 == "quiet" { print $3 }' $runs_table | sort -n))
  loaded_runs=($(awk -F'\t' -v mode=$mode '$1 == mode && $10 == "loaded" { print $3 }' $runs_table | sort -n))
  if (( ${#quiet_runs} > 0 )); then
    printf '  %-5s quiet best %s, median %s, worst %s (%d of %d runs quiet)\n' $mode \
      $quiet_runs[1] $quiet_runs[$(( (${#quiet_runs} + 1) / 2 ))] $quiet_runs[-1] ${#quiet_runs} $runs
  else
    outcome=3
    printf '  %-5s no quiet number: all %d runs started or ended above the load ceiling of %s\n' $mode $runs $ceiling
  fi
  (( ${#loaded_runs} > 0 )) &&
    printf '        loaded runs, not a quiet number: %s\n' "${(j:, :)loaded_runs}"
done
print "runs and logs: $logs"

# The record is written from the same runs table the summary above printed, so the two cannot differ, and
# a mode with no quiet run is recorded with no quiet number.
if [[ $record == true ]]; then
  (cd ${0:A:h:h} && go run ./internal/benchresults/tools/record -runs $runs_table -engine $engine \
    -project ${project:t} -project-commit $project_commit -ceiling $ceiling -settle $settle -rounds $runs \
    -edit $edit) || fail "could not record the run; its logs are in $logs"
fi
exit $outcome
