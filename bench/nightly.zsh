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
# The night runs inside a quiet window of its own, the wall every cohere-dev job meets (command/cohere-dev,
# quiet.go): other members' pool jobs held the cores through a whole night on 2026-11-11, so ahra never got a
# quiet run. Before the first project:
#   wait     a window someone else holds is never overwritten or extended: the guard waits for it to close,
#            up to its own deadline, and if it never does, each project's night is lost, naming that window
#   open     `cohere-dev quiet on --for 20m --through speed-guard`, which refuses every pool job but this one.
#            The 20 minutes are the cap: a hung guard's window expires on its own, and every exit, a signal
#            among them, closes it sooner through the trap below
#   drain    jobs that were running when the window opened finish as they were, so the bench waits until
#            every token is free, up to a deadline counted inside the window, or each night is lost, naming
#            the jobs still running
# Each tonight file carries the window: when it opened and closed, how long it held, what it waited for, and
# the processes using the most CPU right after the bench, which inside the window ran outside the pool.
#
# For each project, in order:
#   bench    bench/quiet_machine.sh, cold only, at most 5 rounds, its record kept in the state directory
#            rather than bench/results, which holds only records someone chose to commit
#   guard    the record against the baseline (Pass, Regressed, Faster or Unmeasured), or the lost night
#   timing   one more cold run of the bench's pinned engine in the bench's copy, outside the timed runs,
#            with --timing, whose rule table says which rules a slower night spent its time in: on a
#            regressed night the ten rules whose CPU grew most against the baseline's timing
#   tonight  <state>/tonight-<project>.json: the guard's result, the commits between the baseline and tonight
#            in cohere and in the project on a night that is not a pass, the timing growth, the bench's log
#
# usage: nightly.zsh [options]
#   --state DIR          where the baselines, verdicts, records, logs and timings live
#                        (default ~/Library/Application Support/cohere/speed-guard)
#   --cohere PATH        the cohere launcher, or an engine binary, the bench runs (default: `cohere` on PATH)
#   --idle-floor PERCENT the share of all cores idle before and after a quiet run (default 90)
#   --projects LIST      the projects to measure, comma-separated, from the table below (default ahra,cohere)
#
# Each default can also come from the environment, COHERE_SPEED_GUARD_STATE, _COHERE, _IDLE_FLOOR and _PROJECTS,
# so a test fire of the AhraOS nerve that runs this script with no arguments (runCohereSpeedGuard) can point
# it at a scratch state and a planted engine, never the real guard's. A flag still wins over its variable.
# The window's minutes come only from the environment, for a proof that shrinks them:
# COHERE_SPEED_GUARD_WINDOW_MINUTES, its cap (20), _WINDOW_WAIT_MINUTES, how long another's window is waited
# for (15), and _DRAIN_MINUTES, how long the pool is waited for inside the window (8, which leaves the
# benches, about 11 minutes on a quiet machine, most of the 20).
#
# Exit status: 0 when every project got its tonight file, whatever its verdict, 1 otherwise. A regressed night
# is the guard's answer, not the script's failure, and is read from the tonight file.
set -u
zmodload zsh/datetime
name=nightly
fail() { print -u2 "$name: $*"; exit 1; }

state=${COHERE_SPEED_GUARD_STATE:-$HOME/Library/Application Support/cohere/speed-guard}
cohere=${COHERE_SPEED_GUARD_COHERE:-cohere}
idle_floor=${COHERE_SPEED_GUARD_IDLE_FLOOR:-90}
projects_asked=${COHERE_SPEED_GUARD_PROJECTS:-ahra,cohere}
window_minutes=${COHERE_SPEED_GUARD_WINDOW_MINUTES:-20}
window_wait_minutes=${COHERE_SPEED_GUARD_WINDOW_WAIT_MINUTES:-15}
drain_minutes=${COHERE_SPEED_GUARD_DRAIN_MINUTES:-8}
[[ $window_minutes == <1-240> ]] || fail "COHERE_SPEED_GUARD_WINDOW_MINUTES is $window_minutes, want 1 to 240"
[[ $window_wait_minutes == <0-> ]] || fail "COHERE_SPEED_GUARD_WINDOW_WAIT_MINUTES is $window_wait_minutes, want 0 or more"
[[ $drain_minutes == <0-> ]] || fail "COHERE_SPEED_GUARD_DRAIN_MINUTES is $drain_minutes, want 0 or more"

while (( $# > 0 )); do
  case $1 in
    --state) state=$2; shift 2 ;;
    --cohere) cohere=$2; shift 2 ;;
    --idle-floor) idle_floor=$2; shift 2 ;;
    --projects) projects_asked=$2; shift 2 ;;
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

state=${state:A}
mkdir -p $state/records $state/logs $state/timing || fail "could not make the state directory $state"
night=$(date +%Y-%m-%d)
repository=${0:A:h:h}

# The guard and cohere-dev are built once, from this script's own checkout, as quiet_machine.sh builds its run
# output reader, and removed when the night ends however it ends. Both are built before the window opens, so
# the window never waits on a build, and the trap that closes it runs a binary rather than compiling one.
tools=$(mktemp -d ${TMPDIR:-/tmp}/cohere-nightly.XXXXXX) || fail "could not make a directory for the guard"
guard=$tools/guard
cohere_dev=$tools/cohere-dev

# The night's quiet window, as the tonight files record it: when it opened and its cap ends, in epoch
# seconds, its holder (the end of the line cohere-dev quiet prints for it, which says it is still this
# guard's), when and how it closed, and what the guard waited for before its first run.
window_through=speed-guard
window_holder="nightly speed guard, pid $$"
window_opened_at=
window_until=
window_closed_at=
window_closed_by=
window_waited_for=
window_waited_seconds=0
pool_jobs_waited_for=()
pool_waited_seconds=0
tonight_files=()

# close_window releases the window once, and only while it is still this guard's: one that expired at its cap
# and was opened by someone since is theirs. closedBy is QuietOff when this guard released it, Cap when its
# --for ran out first, and Removed when it was gone before its cap by someone else's quiet off.
close_window() {
  [[ -n $window_opened_at && -z $window_closed_at ]] || return 0
  window_closed_at=$EPOCHSECONDS
  local line
  line=$($cohere_dev quiet 2>&1)
  [[ $line == *": $window_holder" ]] && $cohere_dev quiet off >&2
  if (( window_closed_at >= window_until )); then
    window_closed_by=Cap
    window_closed_at=$window_until
  elif [[ $line == *": $window_holder" ]]; then
    window_closed_by=QuietOff
  else
    window_closed_by=Removed
  fi
}

# finish runs on every exit: the window closes before the tools it closes with are removed. A signal exits
# through it too, with the signal's own status.
finish() {
  close_window
  rm -f $guard $cohere_dev
  rmdir $tools 2> /dev/null
}
trap finish EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

(cd $repository && go build -o $guard ./internal/bench_results/tools/guard) || fail "could not build the guard"
(cd $repository && go build -o $cohere_dev ./command/cohere-dev) || fail "could not build cohere-dev"

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

# open_window prints the quiet window open now, as cohere-dev quiet describes it (who opened it, where, why and
# when it ends), or nothing when none is open. A quiet that fails is printed as a window, since it can't say
# none holds.
open_window() {
  local line
  if ! line=$($cohere_dev quiet 2>&1); then
    print -r -- "cohere-dev quiet failed: ${line//$'\n'/ }"
  elif [[ $line == "quiet window: open"* ]]; then
    print -r -- $line
  fi
}

# running_jobs prints, one a line, what holds each pool token that is not free, as cohere-dev status names it.
# A status that fails is printed as a job, since it can't say the pool is drained.
running_jobs() {
  local pool_status
  if ! pool_status=$($cohere_dev status 2>&1); then
    print -r -- "cohere-dev status failed: ${pool_status//$'\n'/ }"
    return
  fi
  sed -n 's/^  token [0-9]*: //p' <<< $pool_status | grep -v '^free$'
}

# busiest_processes prints, as JSON, the ten processes using the most CPU over a second, top's second sample,
# since its first measures nothing. Inside the window no pool job but this guard's starts, so what is heavy
# here, besides the bench's own, ran outside the pool. A command name may hold spaces, so a row is read from
# its end.
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

# window_json prints the night's quiet window as a tonight file records it, with busiest, the processes
# sampled after the project's bench. A window still open says so, with how long it has held so far; the
# night's end writes when it closed into each tonight file once it has.
window_json() {
  jq -n --arg through $window_through --arg openedAt "$window_opened_at" --arg until "$window_until" \
    --argjson now $EPOCHSECONDS --arg waitedFor "$window_waited_for" \
    --argjson waitedForSeconds $window_waited_seconds --argjson poolSeconds $pool_waited_seconds \
    --argjson busiest $1 '
    (if $openedAt == "" then null else $openedAt | tonumber end) as $opened |
    {
      through: $through,
      openedAt: (if $opened == null then null else $opened | todate end),
      capAt: (if $until == "" then null else $until | tonumber | todate end),
      closedAt: (if $opened == null then null else "still open" end),
      closedBy: null,
      heldSeconds: (if $opened == null then 0 else $now - $opened end),
      waitedForWindow: (if $waitedFor == "" then null else $waitedFor end),
      waitedForWindowSeconds: $waitedForSeconds,
      poolJobsWaitedFor: $ARGS.positional,
      waitedForPoolSeconds: $poolSeconds,
      busiestAfterBench: $busiest
    }' --args -- $pool_jobs_waited_for
}

# measure_project runs one project's night, writes its tonight file, and prints its summary line. Given a
# reason, the night is lost before its bench, for that reason, which its log holds in place of the bench's.
# It returns nonzero when no tonight file was written, and never stops the projects after it.
measure_project() {
  local project=$1 lost=${2:-} checkout bench_exit guard_exit guard_output record= reason
  project_arguments $project
  local bench_arguments=($reply)
  checkout=${bench_arguments[2]}
  local log=$state/logs/$night-$project.log
  # A directory of its own for each run's record: a record's file name is its date, its engine's commit and
  # its machine, and not its project, so the projects of one night, or a night run twice, would otherwise
  # collide, and the record tool refuses to overwrite one.
  local records=$state/records/$project/$(date +%Y%m%d-%H%M%S)

  if [[ -n $lost ]]; then
    print -r -- "$name: no bench tonight: $lost" > $log
  else
    mkdir -p $records || { print -u2 "$name: $project: could not make $records"; return 1; }
    # The run the window is for: a cohere-dev job the bench starts passes the wall as this guard's own.
    COHERE_DEV_QUIET_THROUGH=$window_through zsh $repository/bench/quiet_machine.sh $bench_arguments \
      --modes cold --runs 5 --record-to $records --cohere $cohere --idle-floor $idle_floor > $log 2>&1
    bench_exit=$?
    # 0 is a quiet number and 3 is none; either way the bench recorded the run. The record is the newest of
    # this project's in this run's own directory, so it was written after the run started.
    if (( bench_exit == 0 || bench_exit == 3 )); then
      local candidate
      for candidate in $records/*.json(N.om); do
        [[ $(jq -r .project.name $candidate) == ${checkout:t} ]] && { record=$candidate; break; }
      done
    fi
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
    if [[ -n $lost ]]; then
      reason=$lost
    else
      reason="the bench exited $bench_exit, with no record; $(tail -1 $log)"
    fi
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
      [[ $(jq -r .reason <<< $guard_output) == "the first quiet night, now the baseline" ]] &&
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
    --argjson timingGrowth $timing_growth_json --argjson window "$(window_json $busiest_json)" \
    '. + {night: $night, log: $log, cohereCommits: $cohereCommits, projectCommits: $projectCommits,
      timingGrowth: $timingGrowth, timingNote: (if $timingNote == "" then null else $timingNote end),
      window: $window}' \
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

# Another's window is waited for, never taken: quiet on would overwrite it, so it is only opened once none
# holds. lost is why every project's night is lost before its bench, empty while the night can still run.
lost=
started=$EPOCHSECONDS
other=$(open_window)
if [[ -n $other ]]; then
  window_waited_for=$other
  print -u2 "$name: waiting up to $window_wait_minutes minutes for another's quiet window to close: $other"
  while [[ -n $other ]] && (( EPOCHSECONDS - started < window_wait_minutes * 60 )); do
    sleep 5
    other=$(open_window)
  done
  window_waited_seconds=$(( EPOCHSECONDS - started ))
  if [[ -n $other ]]; then
    window_waited_for=$other
    lost="another's quiet window held for all $window_wait_minutes minutes this guard waits, and the guard never"
    lost+=" overwrites or extends another's window: $other"
  fi
fi

# The window is this guard's from before quiet on runs, so a signal that lands while it opens still closes it:
# close_window releases it only once cohere-dev quiet names this guard as its holder.
if [[ -z $lost ]]; then
  window_opened_at=$EPOCHSECONDS
  window_until=$(( window_opened_at + window_minutes * 60 ))
  if opened=$(cd $repository &&
    $cohere_dev quiet on --for ${window_minutes}m --through $window_through $window_holder 2>&1); then
    print -u2 "$name: $opened"
  else
    window_opened_at=
    window_until=
    lost="the guard's quiet window would not open: ${opened//$'\n'/ }"
  fi
fi

# The pool drains: a job running when the window opened finishes as it was, and a bench beside it is loaded.
if [[ -z $lost ]]; then
  started=$EPOCHSECONDS
  pool_jobs_waited_for=(${(f)"$(running_jobs)"})
  running=($pool_jobs_waited_for)
  (( ${#running} > 0 )) &&
    print -u2 "$name: waiting up to $drain_minutes minutes for ${#running} pool tokens to come free"
  while (( ${#running} > 0 && EPOCHSECONDS - started < drain_minutes * 60 )); do
    sleep 5
    running=(${(f)"$(running_jobs)"})
  done
  pool_waited_seconds=$(( EPOCHSECONDS - started ))
  if (( ${#running} > 0 )); then
    lost="the pool never drained in the $drain_minutes minutes this guard waits inside its quiet window; still"
    lost+=" running: ${(j:; :)running}"
  fi
fi

outcome=0
for project in $projects; do
  measure_project $project "$lost" || outcome=1
done

# Each tonight file was written with the window still open, so once it closes, each says when and how.
close_window
if [[ -n $window_opened_at ]]; then
  for tonight_file in $tonight_files; do
    jq --argjson closedAt $window_closed_at --arg closedBy $window_closed_by \
      --argjson heldSeconds $(( window_closed_at - window_opened_at )) \
      '.window += {closedAt: ($closedAt | todate), closedBy: $closedBy, heldSeconds: $heldSeconds}' \
      $tonight_file > $tonight_file.partial &&
      mv $tonight_file.partial $tonight_file ||
      { print -u2 "$name: could not write when the window closed into $tonight_file"; outcome=1; }
  done
fi
exit $outcome
