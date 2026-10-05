#!/bin/zsh
# The quiet-machine benchmark: how long `cohere` takes on a real project, cold, on an unchanged replay,
# and after a one-file edit, measured so that the number means what it says.
#
# It runs at the priority it is started with, never niced as the house's test and build runs are
# (cohere-dev test, #2qc6j8g): it measures, and a niced run's times say how busy the machine was rather
# than how fast cohere is. Run it only in a quiet window, where nothing else is competing.
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
#           replays, up to three times. A second prime is expected every round after the first: the edit
#           run's cache entry holds the edited bytes, so the next run rechecks the restored file and its
#           importers. Each run's row carries how many primes its round needed, every prime's cache use
#           is kept in primes.tsv, and the summary warns on what is actually wrong: a prime after the first
#           round that took nothing from the cache (#547dhjz), or a warm run that is not a whole replay
#   warm    `cohere` again on the unchanged tree: the replay
#   edit    one line added to one function body, `cohere`, then the file's original bytes put back
#   cold    `cohere --no-cache`: every phase from source, nothing read or written
#   first   `cohere` in a fresh copy of the tree with no cache of any kind: what a developer's first run
#           pays, cache writes and incremental build info included. Measured beside cold, because a first
#           run doing more than cold is a cliff nothing else here would show: on 2026-10-04 a first run
#           on www-ahra-ai took 8.6s against 0.8s cold (#0q6nmnt). Its runs go to first.tsv, apart from
#           the runs table the record reads, and the summary flags a quiet median more than 25% over
#           cold's
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
#                      numbers, refused when it disagrees with its own runs or the engine is dirty;
#                      commit it with what `go run ./internal/docsdata/tools/generate` rewrites
#
# Exit status: 0 when every mode has a quiet number, 3 when some mode has none, 4 when every mode has one
# and the first run's quiet median is more than 25% over cold's, 1 when it could not measure at all,
# including a copy that changed under it or a cohere that changed between runs.
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

# The first mode's template: the copy without its cache, made once per copy and cloned for each first run,
# so a first run never sees what an earlier run of any mode left behind. Its tree is the copy's, or it is
# refused, the same as the copy.
template=$work/${copy:t}-first-template
if [[ ! -f $template/bench-ready ]]; then
  [[ -e $template ]] && fail "$template exists but was never finished; remove it and run again"
  mkdir -p $template
  rsync -a --exclude=/.cache "$copy/" "$template/" || fail "copying the copy into $template"
fi
[[ $(copy=$template tree_hash) == $expected_tree ]] ||
  fail "the first runs' template at $template no longer matches the copy; remove it and run again"

logs=$copy/.cache/quiet-machine-$(date +%Y%m%d-%H%M%S)
mkdir -p $logs
runs_table=$logs/runs.tsv
pin_engine $logs
first_table=$logs/first.tsv
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
  local table=${measure_table:-$runs_table}
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
    tee -a $table
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
    prime_cache=$($run_output_reader $logs/prime-$round-$prime.log | cut -f3) ||
      fail "could not read the prime's output, see $logs/prime-$round-$prime.log"
    printf '%d\t%d\t%s\n' $round $prime "$prime_cache" >> $logs/primes.tsv
    [[ $prime_cache == replay ]] && break
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

  # A fresh clone of the template for each first run (copy-on-write where the volume allows it), measured
  # there, then held to the copy's tree: a first run must write nothing but its cache.
  first_copy=$logs/first-$round
  cp -c -R $template $first_copy 2> /dev/null || cp -R $template $first_copy || fail "cloning $template"
  copy=$first_copy measure_table=$first_table measure first $round
  [[ $(copy=$first_copy tree_hash) == $expected_tree ]] ||
    fail "the first run in round $round changed the tree it ran in; see $first_copy"
done

[[ $(tree_hash) == $expected_tree ]] ||
  fail "the copy's files changed during the benchmark, so the runs did not all see the same tree; see $logs"

# A warm and an edit run in the same round should report the same findings; when they do not, the edit
# changed more than bytes.
awk -F'\t' '$1 == "warm" { warm[$2] = $6 } $1 == "edit" { edit[$2] = $6 }
  END { for (round in edit) if (edit[round] != warm[round])
    printf "note: round %s edit reported %s findings and warm %s, so the edit was not neutral\n", round, edit[round], warm[round] }' $runs_table

# What the primes and warm runs should never do. After the first round the cache holds the last round, so a
# prime that took nothing from it is #547dhjz, a cache the run before left unusable. A warm run is a whole
# replay or the benchmark measured something else under its name. The first round's first prime may start
# cold, after a new engine, and is not warned on.
awk -F'\t' '$1 > 1 && $3 == "none" {
    printf "warning: round %s prime %s took nothing from the cache (#547dhjz); see prime-%s-%s.log\n", $1, $2, $1, $2 }' $logs/primes.tsv
awk -F'\t' '$1 == "warm" && $7 != "replay" {
    printf "warning: round %s warm run read cache %s, not a whole replay\n", $2, $7 }' $runs_table

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

# The first mode, from its own table, and the one comparison it exists for (#0q6nmnt).
first_quiet=($(awk -F'\t' '$10 == "quiet" { print $3 }' $first_table | sort -n))
first_loaded=($(awk -F'\t' '$10 == "loaded" { print $3 }' $first_table | sort -n))
cold_quiet=($(awk -F'\t' '$1 == "cold" && $10 == "quiet" { print $3 }' $runs_table | sort -n))
if (( ${#first_quiet} > 0 )); then
  first_median=$first_quiet[$(( (${#first_quiet} + 1) / 2 ))]
  printf '  %-5s quiet best %s, median %s, worst %s (%d of %d runs quiet)\n' first \
    $first_quiet[1] $first_median $first_quiet[-1] ${#first_quiet} $runs
  if (( ${#cold_quiet} > 0 )); then
    cold_median=$cold_quiet[$(( (${#cold_quiet} + 1) / 2 ))]
    if awk -v first=$first_median -v cold=$cold_median 'BEGIN { exit !(first > cold * 1.25) }'; then
      printf 'warning: the first run'"'"'s quiet median %s is more than 25%% over cold'"'"'s %s: a first run is doing work cold does not (#0q6nmnt); see first-*.log\n' \
        $first_median $cold_median
      (( outcome == 0 )) && outcome=4
    fi
  fi
else
  outcome=3
  printf '  %-5s no quiet number: all %d runs started or ended above the load ceiling of %s\n' first $runs $ceiling
fi
(( ${#first_loaded} > 0 )) &&
  printf '        loaded runs, not a quiet number: %s\n' "${(j:, :)first_loaded}"
print "runs and logs: $logs"

# The record is written from the same runs table the summary above printed, so the two cannot differ, and
# a mode with no quiet run is recorded with no quiet number.
if [[ $record == true ]]; then
  (cd ${0:A:h:h} && go run ./internal/benchresults/tools/record -runs $runs_table -engine $engine \
    -project ${project:t} -project-commit $project_commit -ceiling $ceiling -settle $settle -rounds $runs \
    -edit $edit) || fail "could not record the run; its logs are in $logs"
  # The website's benchmarks page is generated from bench/results, and a record committed alone leaves
  # docs/data stale, which TestTheRenderedFilesAreCurrent fails (@system_cohere_web_data).
  print "commit the record with what \`go run ./internal/docsdata/tools/generate\` rewrites in docs/data"
fi
exit $outcome
