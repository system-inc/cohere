#!/bin/zsh
# The nightly speed guard: each night, a cold quiet-machine benchmark of each project, held against that
# project's baseline of record, so a change that makes cohere slower is caught the morning after it lands
# rather than when someone happens to notice.
#
# The comparison is never against last night. A guard that compared nights would let a creep of a few percent
# a night through every night, one small step at a time, so each night is held against the baseline, which
# moves only by hand, with a reason on record (internal/bench_results/tools/guard, -accept). A project's first
# quiet night becomes its baseline by itself, so the guard starts with nothing to set up.
#
# A night is only a number when the machine was quiet. A night the machine was never quiet is Unmeasured, never
# a pass, and so is a night that left no record at all, because the bench refused its copy, crashed or ran out
# of time: every night is on record in the guard's verdicts either way, so a guard that has not measured for a
# week reads as that, with its streak, rather than as a week of green.
#
# The night opens no quiet window. One on this Mac held every repository's test gate, ahra's commit make among
# them, so Kirk ruled on 2026-11-12 that cohere takes none here (#8sqsgee): the guard runs at whatever load the machine
# has, a loaded night is Unmeasured and says so, and each tonight file names the processes using the most CPU right
# after its bench (busiestAfterBench), which says what held the cores. Timing that needs a quiet machine moves to
# Loom, through @system_loom_jobs, when its home for this is ready.
#
# For each project, in order:
#   bench    bench/quiet_machine.sh, cold only, checking without fixing (`--no-fix`), at most 5 rounds, its
#            record kept in the state directory rather than bench/results, which holds only records someone chose to
#            commit. Checking, because a fixing run's work moves with whatever the tree holds that night, and a
#            tree with a fix pending was rewritten under the bench on 2026-11-12, losing the night (#8sqsgee); the
#            guard moved its baseline to the first quiet checking night (internal/bench_results/tools/guard)
#   guard    the record against the baseline (Pass, Regressed, Faster or Unmeasured), or the lost night
#   timing   one more cold run of the bench's pinned engine in the bench's copy, outside the timed runs,
#            with --timing, whose rule table says which rules a slower night spent its time in: on a
#            regressed night the ten rules whose CPU grew most against the baseline's timing
#   tonight  <state>/tonight-<project>.json: the guard's result, the commits between the baseline and tonight
#            in cohere and in the project on a night that is not a pass, the timing growth, the bench's log
#   kept     a copy of each night's tonight file in <state>/nights, and the bench's busy-after samples (top's
#            busiest processes after a run that turned the machine loaded) in <state>/busy/<night>-<project>, so a
#            week of what the machine holds at night outlives the temporary copy, for choosing the idle floor from
#            the nights rather than from one (#8sqsgee). Each run's idleness before and after is in its record.
#
# Then, once every project's bench is done, so it never loads a timed run:
#   isolation the unit-isolation tier (#h350vqr), from this script's own checkout through cohere-dev's pool: every
#            rule's fixtures (COHERE_UNIT_ISOLATION=fixtures), then a sample of cohere's tree under the night's seed,
#            its date as a number (=tree), both logged in <state>/logs/<night>-isolation.log. A pass prints one line
#            beside the projects'. A failure posts the seed and the failing lines to #ewgh523 with ahra's tasks
#            comment, the report kept beside the log, so the failure is run again by its seed.
#   landing  each entry the fast tier leaves to the landing gate measured on this machine's CPU, the measured half
#            of TestEveryLandingGateOnlyEntryStillEarnsItsPlace, which is opt-in and no gate's (#xc29pnd), logged in
#            <state>/logs/<night>-landing-gate.log. A pass prints one line; a failure posts its lines to #crw2hx3.
#
# usage: nightly.zsh [options]
#   --state DIR          where the baselines, verdicts, records, logs and timings live
#                        (default ~/Library/Application Support/cohere/speed-guard)
#   --cohere PATH        the cohere launcher, or an engine binary, the bench runs (default: `cohere` on PATH)
#   --idle-floor PERCENT the share of all cores idle before and after a quiet run (default 90)
#   --projects LIST      the projects to measure, comma-separated, from the table below (default ahra,cohere)
#   --isolation on|off   whether the night runs the isolation tier after the benches (default on)
#   --landing on|off     whether the night measures the landing gate's entries after the benches (default on)
#
# Each default can also come from the environment, COHERE_SPEED_GUARD_STATE, _COHERE, _IDLE_FLOOR, _PROJECTS,
# _ISOLATION and _LANDING,
# so a test fire of the AhraOS nerve that runs this script with no arguments (runCohereSpeedGuard) can point
# it at a scratch state and a planted engine, never the real guard's. A flag still wins over its variable.
#
# Exit status: 0 when every project got its tonight file, whatever its verdict, and a failing isolation tier was
# posted, 1 otherwise. A regressed night is the guard's answer, not the script's failure, and is read from the tonight
# file; a failing isolation tier is the tier's answer, read from #ewgh523.
set -u
zmodload zsh/datetime
name=nightly
fail() { print -u2 "$name: $*"; exit 1; }

state=${COHERE_SPEED_GUARD_STATE:-$HOME/Library/Application Support/cohere/speed-guard}
cohere=${COHERE_SPEED_GUARD_COHERE:-cohere}
idle_floor=${COHERE_SPEED_GUARD_IDLE_FLOOR:-90}
projects_asked=${COHERE_SPEED_GUARD_PROJECTS:-ahra,cohere}
isolation=${COHERE_SPEED_GUARD_ISOLATION:-on}
landing=${COHERE_SPEED_GUARD_LANDING:-on}

while (( $# > 0 )); do
  case $1 in
    --state) state=$2; shift 2 ;;
    --cohere) cohere=$2; shift 2 ;;
    --idle-floor) idle_floor=$2; shift 2 ;;
    --projects) projects_asked=$2; shift 2 ;;
    --isolation) isolation=$2; shift 2 ;;
    --landing) landing=$2; shift 2 ;;
    *) fail "unknown argument $1 (see the usage at the top of this script)" ;;
  esac
done

# project_arguments sets reply to the bench's arguments for a project in the table, fixed here so every night
# measures the same thing: ahra with its route types generated, as a checkout holds them (see run_generator in
# pinned.zsh), and cohere's own root.
project_arguments() {
  case $1 in
    ahra) reply=(--project $HOME/Projects/ahra --generate 'NEXT_TELEMETRY_DISABLED=1 node_modules/.bin/next typegen') ;;
    cohere) reply=(--project $HOME/Projects/system/cohere) ;;
    *) return 1 ;;
  esac
}

projects=(${(s:,:)projects_asked})
(( ${#projects} > 0 )) || fail "--projects names no project"
for project in $projects; do
  project_arguments $project || fail "--projects: no project $project in the table (ahra, cohere)"
done
[[ $isolation == (on|off) ]] || fail "--isolation must be on or off, not $isolation"
[[ $landing == (on|off) ]] || fail "--landing must be on or off, not $landing"

state=${state:A}
mkdir -p $state/records $state/logs $state/timing || fail "could not make the state directory $state"
night=$(date +%Y-%m-%d)
repository=${0:A:h:h}

# The guard is built once, from this script's own checkout, as quiet_machine.sh builds its run output reader, and
# removed when the night ends however it ends.
tools=$(mktemp -d ${TMPDIR:-/tmp}/cohere-nightly.XXXXXX) || fail "could not make a directory for the guard"
guard=$tools/guard

tonight_files=()

# finish runs on every exit, removing the guard. A signal exits through it too, with the signal's own status.
finish() {
  rm -f $guard
  rmdir $tools 2> /dev/null
}
trap finish EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

(cd $repository && go build -o $guard ./internal/bench_results/tools/guard) || fail "could not build the guard"

# parse_timing prints a --timing output's rule table as rule and CPU milliseconds, tab-separated, sorted by
# rule: the rows after each header whose first fields are `rule` and `cpu`, up to the blank line that ends the
# table. A row whose CPU is not a duration (the Go analyzers' `shared:` lines) is not a rule's. A rule in two
# tables, one per engine, is the sum of its rows.
parse_timing() {
  awk '
    table && /^[ \t]*$/ { table = 0; next }
    table && $2 ~ /^[0-9.]+(ms|s)$/ {
      value = $2
      if (value ~ /ms$/) { sub(/ms$/, "", value); milliseconds = value + 0 }
      else { sub(/s$/, "", value); milliseconds = (value + 0) * 1000 }
      cpu[$1] += milliseconds
      next
    }
    $1 == "rule" && $2 == "cpu" { table = 1 }
    END { for (rule in cpu) printf "%s\t%.3f\n", rule, cpu[rule] }' $1 | sort
}

# timing_growth prints the ten rules whose CPU grew most from the baseline's timing to tonight's, as JSON: a
# rule tonight that the baseline did not run grew from nothing.
timing_growth() {
  local baseline=$1 tonight=$2
  awk -F'\t' 'NR == FNR { baseline[$1] = $2; next }
    { printf "%s\t%.3f\t%.3f\t%.3f\n", $1, $2 - baseline[$1], baseline[$1] + 0, $2 }' $baseline $tonight |
    sort -t$'\t' -k2,2 -g -r | head -10 |
    jq -R 'split("\t") |
      {rule: .[0], baselineMilliseconds: (.[2] | tonumber), tonightMilliseconds: (.[3] | tonumber)}' |
    jq -s .
}

# commits_between prints, as JSON, the commits in <checkout> after <baseline> up to <tonight>, the newest 30
# of them and how many there are, or why there are none to show.
commits_between() {
  local checkout=$1 baseline=$2 tonight=$3 count
  if ! count=$(git -C $checkout rev-list --count $baseline..$tonight 2> /dev/null); then
    jq -n --arg note "$checkout does not have both $baseline and $tonight" '{count: null, commits: [], note: $note}'
    return
  fi
  git -C $checkout log --oneline $baseline..$tonight | head -30 | jq -R . | jq -s --argjson count $count \
    '{count: $count, commits: .}'
}

# busiest_processes prints, as JSON, the ten processes using the most CPU over a second, top's second sample,
# since its first measures nothing: what held the cores right after a bench, the bench's own tail aside. A command
# name may hold spaces, so a row is read from its end.
busiest_processes() {
  top -l 2 -s 1 -n 10 -o cpu -stats command,pid,cpu |
    awk '/^COMMAND/ { sample++; next }
      sample == 2 && NF >= 3 {
        command = $1
        for (field = 2; field < NF - 1; field++) command = command " " $field
        printf "%s\t%s\t%s\n", command, $(NF - 1), $NF
      }' |
    jq -R 'split("\t") | {command: .[0], pid: (.[1] | tonumber? // .[1]), cpuPercent: (.[2] | tonumber? // .[2])}' |
    jq -s .
}

# measure_project runs one project's night, writes its tonight file, and prints its summary line. It returns
# nonzero when no tonight file was written, and never stops the projects after it.
measure_project() {
  local project=$1 checkout bench_exit guard_exit guard_output record= reason
  project_arguments $project
  local bench_arguments=($reply)
  checkout=${bench_arguments[2]}
  local log=$state/logs/$night-$project.log
  # A directory of its own for each run's record: a record's file name is its date, its engine's commit and
  # its machine, and not its project, so the projects of one night, or a night run twice, would otherwise
  # collide, and the record tool refuses to overwrite one.
  local records=$state/records/$project/$(date +%Y%m%d-%H%M%S)

  mkdir -p $records || { print -u2 "$name: $project: could not make $records"; return 1; }
  zsh $repository/bench/quiet_machine.sh $bench_arguments \
    --modes cold --no-fix --runs 5 --record-to $records --cohere $cohere --idle-floor $idle_floor > $log 2>&1
  bench_exit=$?
  # 0 is a quiet number and 3 is none; either way the bench recorded the run. The record is the newest of
  # this project's in this run's own directory, so it was written after the run started.
  if (( bench_exit == 0 || bench_exit == 3 )); then
    local candidate
    for candidate in $records/*.json(N.om); do
      [[ $(jq -r .project.name $candidate) == ${checkout:t} ]] && { record=$candidate; break; }
    done
  fi
  local busiest_json=$(busiest_processes)

  if [[ -n $record ]]; then
    guard_output=$($guard -state $state -tonight $record 2> $records/guard-error)
    guard_exit=$?
    # 3 is a regressed night, the guard's answer. Any other failure means tonight's record could not be held
    # against the baseline (another machine, a record that does not validate), which is a night with no
    # number, and is recorded as one rather than leaving the night off the verdicts.
    if (( guard_exit != 0 && guard_exit != 3 )); then
      reason="the guard refused tonight's record $record: $(tail -1 $records/guard-error)"
      record=
      guard_output=$($guard -state $state -project $project -lost $reason) ||
        { print -u2 "$name: $project: could not record the lost night"; return 1; }
    fi
  else
    reason="the bench exited $bench_exit, with no record; $(tail -1 $log)"
    guard_output=$($guard -state $state -project $project -lost $reason) ||
      { print -u2 "$name: $project: could not record the lost night"; return 1; }
  fi
  local verdict=$(jq -r .verdict <<< $guard_output)

  # The timing run, in the bench's copy with its pinned engine, read from the bench's log: the header names the
  # copy and the summary names the logs directory the engine was copied into.
  local timing=$state/timing/$night-$project.txt
  local timing_table=$state/timing/$night-$project.tsv
  local baseline_timing=$state/timing/baseline-$project.tsv
  local timing_note= timing_growth_json='[]'
  local copy=$(sed -n 's/^# copy     //p' $log | head -1)
  local logs=$(sed -n 's/^runs and logs: //p' $log | head -1)
  # The busy-after samples live in the copy, which a reboot or the next copy clears; kept, they are the nights' answer
  # to what held the cores.
  local busy=($logs/*.busy-after(N))
  if (( ${#busy} > 0 )); then
    mkdir -p $state/busy/$night-$project && cp $busy $state/busy/$night-$project/ ||
      print -u2 "$name: $project: could not keep the busy-after samples in $state/busy"
  fi
  if [[ -z $record ]]; then
    timing_note="no timing: the night left no record to time against"
  elif [[ ! -d $copy || ! -x $logs/cohere-engine ]]; then
    timing_note="no timing: the bench's log names no copy or pinned engine"
  else
    (
      cd $copy || exit 1
      unset COHERE_SWIFT_ENGINE
      [[ -x $logs/cohere-swift ]] && export COHERE_SWIFT_ENGINE=$logs/cohere-swift
      $logs/cohere-engine --json --no-fix --no-cache --timing
    ) > $timing 2>&1
    parse_timing $timing > $timing_table
    if [[ ! -s $timing_table ]]; then
      timing_note="no timing: $timing holds no rule table with a rule and cpu header"
    else
      # A baseline's timing is taken the night it became the baseline, so the two describe the same run.
      local baseline_reason=$(jq -r .reason <<< $guard_output)
      [[ $baseline_reason == ("the first quiet night, now the baseline"|"the instrument changed:"*) ]] &&
        cp $timing_table $baseline_timing
      if [[ $verdict == Regressed ]]; then
        if [[ -s $baseline_timing ]]; then
          timing_growth_json=$(timing_growth $baseline_timing $timing_table)
        else
          timing_note="no timing growth: the baseline has no timing, since this script did not time the night it"
          timing_note+=" became the baseline"
        fi
      fi
    fi
  fi

  # The commits since the baseline, on a night that is not a pass or faster: what landed in cohere and in the
  # project between the baseline and tonight is where a regression, or a run of lost nights, came from.
  local cohere_commits=null project_commits=null
  local baseline_cohere=$(jq -r .baselineCohereCommit <<< $guard_output)
  local tonight_cohere=$(jq -r .tonightCohereCommit <<< $guard_output)
  local baseline_project=$(jq -r .baselineProjectCommit <<< $guard_output)
  local tonight_project=$(jq -r .tonightProjectCommit <<< $guard_output)
  if [[ $verdict == (Regressed|Unmeasured) ]]; then
    [[ -n $baseline_cohere && -n $tonight_cohere ]] &&
      cohere_commits=$(commits_between $repository $baseline_cohere $tonight_cohere)
    [[ -n $baseline_project && -n $tonight_project ]] &&
      project_commits=$(commits_between $checkout $baseline_project $tonight_project)
  fi

  # Written beside itself and moved into place, so a reader never opens half of one.
  local tonight_file=$state/tonight-$project.json
  jq --arg night $night --arg log $log --arg timingNote "$timing_note" \
    --argjson cohereCommits $cohere_commits --argjson projectCommits $project_commits \
    --argjson timingGrowth $timing_growth_json --argjson busiest $busiest_json \
    '. + {night: $night, log: $log, cohereCommits: $cohereCommits, projectCommits: $projectCommits,
      timingGrowth: $timingGrowth, timingNote: (if $timingNote == "" then null else $timingNote end),
      busiestAfterBench: $busiest}' \
    <<< $guard_output > $tonight_file.partial &&
    mv $tonight_file.partial $tonight_file ||
    { print -u2 "$name: $project: could not write $tonight_file"; return 1; }
  tonight_files+=($tonight_file)

  jq -r --arg project $project '"\($project): \(.verdict)" +
    (if .ratio > 0 then ", \(.tonightMedianSeconds)s against \(.baselineMedianSeconds)s, " +
      "ratio \(.ratio * 1000 | round / 1000)" else "" end) +
    (if .reason != null and .reason != "" then ", \(.reason)" else "" end) +
    ", unmeasured streak \(.unmeasuredStreak)"' $tonight_file
}

# isolation_report writes the report a failing isolation tier posts to #ewgh523: the seed and which runs failed,
# each failing test and the lines that say what differed, at most 60 of them, and where the whole log is.
isolation_report() {
  local log=$1 seed=$2 report=$3 failed=$4
  {
    print "Nightly unit isolation (#h350vqr) failed on $night, seed $seed: $failed."
    print
    print "Run it again from cohere's root, the fixtures, and the tree by its seed:"
    print -r -- '- `COHERE_UNIT_ISOLATION=fixtures go run ./command/cohere-dev test -count=1'\
' -run TestEveryIsolatedRuleFindsTheSameInTheUnitsAVariantKeeps ./internal/go_engine/check`'
    print -r -- '- `COHERE_UNIT_ISOLATION=tree COHERE_UNIT_ISOLATION_SEED='$seed' go run ./command/cohere-dev test'\
' -count=1 -run TestASampleOfCoheresTreeFindsTheSameInTheUnitsAVariantKeeps ./internal/go_engine/check`'
    print
    grep -E -e '--- FAIL|unit_isolation_test\.go:[0-9]+: ' $log | grep -v -E -e '--- FAIL: .*/fixtures \(' | head -60 |
      sed 's/^ */- /'
    print
    print "The whole log: $log"
  } > $report
}

# run_isolation runs the isolation tier (see isolation, at the top) and prints its line, posting a failure's report to
# #ewgh523. It returns nonzero only when a failure could not be posted.
run_isolation() {
  local log=$state/logs/$night-isolation.log report=$state/logs/$night-isolation-report.md seed=${night//-/}
  local failed=()
  (
    cd $repository &&
      COHERE_UNIT_ISOLATION=fixtures go run ./command/cohere-dev test -count=1 \
        -run '^TestEveryIsolatedRuleFindsTheSameInTheUnitsAVariantKeeps$' ./internal/go_engine/check
  ) > $log 2>&1 || failed+=(fixtures)
  (
    cd $repository &&
      COHERE_UNIT_ISOLATION=tree COHERE_UNIT_ISOLATION_SEED=$seed go run ./command/cohere-dev test -count=1 \
        -run '^TestASampleOfCoheresTreeFindsTheSameInTheUnitsAVariantKeeps$' ./internal/go_engine/check
  ) >> $log 2>&1 || failed+=("the tree")
  if (( ${#failed} == 0 )); then
    print "isolation: passed, every rule's fixtures and the tree under seed $seed, log $log"
    return 0
  fi
  isolation_report $log $seed $report "${(j:, then :)failed}"
  # ahra resolves only from its own checkout, and the night's PATH may not hold it, so its own launcher there.
  if ! (cd $HOME/Projects/ahra && node_modules/.bin/ahra tasks comment ewgh523 --role Agent --text-file $report) \
    >> $log 2>&1; then
    print -u2 "$name: isolation failed under seed $seed, and posting $report to #ewgh523 failed too; see $log"
    return 1
  fi
  print "isolation: failed under seed $seed (${(j:, then :)failed}), posted to #ewgh523, log $log"
}

# run_landing_gate measures each landing-gate-only entry (see landing, at the top) and prints its line, posting a
# failure's lines to #crw2hx3. It returns nonzero only when a failure could not be posted.
run_landing_gate() {
  local log=$state/logs/$night-landing-gate.log report=$state/logs/$night-landing-gate-report.md
  if (
    cd $repository &&
      # An hour, against the measurement's 145s at load 29: its inner runs inherit what is left of this deadline, and
      # at Go's default ten minutes a loaded machine left one 7s and it failed on time, measuring nothing (#6n6nprh).
      COHERE_MEASURE_LANDING_GATE=1 go run ./command/cohere-dev test -count=1 -v -timeout=1h \
        -run '^TestEveryLandingGateOnlyEntryStillEarnsItsPlace$' ./command/cohere-dev
  ) > $log 2>&1; then
    print "landing gate: every landing-gate-only entry still costs more than the fast tier's bar, log $log"
    return 0
  fi
  {
    print "Nightly landing-gate measurement (#xc29pnd) failed on $night."
    print
    grep -E -e '--- FAIL|fast_test\.go:[0-9]+: ' $log | head -40 | sed 's/^ */- /'
    print
    print "The whole log: $log"
  } > $report
  if ! (cd $HOME/Projects/ahra && node_modules/.bin/ahra tasks comment crw2hx3 --role Agent --text-file $report) \
    >> $log 2>&1; then
    print -u2 "$name: the landing-gate measurement failed, and posting $report to #crw2hx3 failed too; see $log"
    return 1
  fi
  print "landing gate: failed, posted to #crw2hx3, log $log"
}

outcome=0
for project in $projects; do
  measure_project $project || outcome=1
done

if [[ $isolation == on ]]; then
  run_isolation || outcome=1
fi

if [[ $landing == on ]]; then
  run_landing_gate || outcome=1
fi

# Each night's tonight file kept by its night, since the next night writes over it. See kept, at the top.
mkdir -p $state/nights || outcome=1
for tonight_file in $tonight_files; do
  cp $tonight_file $state/nights/$night-${${tonight_file:t:r}#tonight-}.json ||
    { print -u2 "$name: could not keep $tonight_file in $state/nights"; outcome=1; }
done
exit $outcome
