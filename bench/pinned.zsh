# What the benchmarks in this directory share: a copy of a project pinned to its commits, the one
# cohere engine every run uses, the machine's load, and a timer that waits for a run's last process.
# Sourced, not run. The sourcing script sets `name` (for its messages), `project`, `work`, `cohere`,
# `ceiling` and `settle` first.
#
# The copy. The project's committed tree is extracted (its submodules too, each at the commit the
# parent records) into the work directory, and the project's node_modules are linked in rather than
# copied. So the input is named by commits, not by whatever was on disk, nothing another session is
# editing can move a number, and the project's own caches are never read or written. A copy is reused
# by later invocations at the same commits, and refused if its files no longer match what was extracted.
#
# The copy is a repository at its commits, as a checkout is, because cohere reads it. The Go engine lists a
# workspace's files with `git ls-files --stage`, and the Swift engine reads its inputs at the commit, so on a
# copy that was only the tree both refused to run, and a benchmark timed a run that checked nothing (#7z52pqf).
# A `.git` file in each submodule makes it a nested repository the format walk skips and discovery treats as
# linked, and the root's `.git` directory carries the project's own `info/exclude`, which the format walk
# applies; without those the copy formatted libraries/structure as part of ahra, 5,269 files where a
# developer's run formats 3,198 (#679s763). Each repository's index is its commit's tree, a submodule a
# gitlink at its pin, and its objects are the project's, read through an alternate rather than copied.
#
# The engine. The launcher rebuilds whenever a cohere commit lands, which during a working session is
# every few minutes, so a benchmark run through it can measure two binaries and report one number. The
# engine the launcher resolves at the start is copied beside the logs and run directly.
zmodload zsh/datetime
# This directory, read here at the top of the sourced file, since inside a function $0 is the function's name.
bench_directory=${0:A:h}

fail() { print -u2 "$name: $*"; exit 1; }
load() { sysctl -n vm.loadavg | awk '{print $2}'; }
under_ceiling() { awk -v load=$1 -v ceiling=$ceiling 'BEGIN { exit !(load <= ceiling) }'; }
# idle_over prints the share of all cores idle, in percent, over the next <seconds>. It is iostat's second
# report, which covers that interval (the first covers the time since boot), read from the end of the line,
# where the three load averages follow the cpu columns whatever disks are listed before them.
idle_over() { iostat -c 2 -w $1 | tail -1 | awk '{ print $(NF - 3) }'; }
# wait_for_quiet waits up to `settle` seconds for the load to reach the ceiling, and returns either way.
wait_for_quiet() {
  (( settle > 0 )) || return 0
  local deadline=$(( EPOCHREALTIME + settle ))
  until under_ceiling $(load) || (( EPOCHREALTIME > deadline )); do sleep 5; done
}

# make_repository makes <destination>, which holds <repository>'s tree at <commit>, a repository at that
# commit. Its objects are <repository>'s, through an alternate; its index is the commit's tree, read rather
# than built from the files, so a submodule is a gitlink at exactly the commit the parent records; and its
# HEAD is the commit. The root's git directory is the copy's `.git`. A submodule's is under `.git/modules`,
# named by a `.git` file with a relative path, as a checkout's is, so a clone of the copy (the first runs'
# template) is a repository of its own rather than one reaching back into the copy.
make_repository() {
  local repository=$1 commit=$2 destination=$3
  local within=${destination#$copy}
  within=${within#/}
  local git_directory=$copy/.git${within:+/modules/$within}
  if [[ -z $within ]]; then
    git init --quiet $destination || fail "making $destination a repository"
  else
    mkdir -p ${git_directory:h}
    git init --quiet --separate-git-dir=$git_directory $destination || fail "making $destination a repository"
    local up=${(j::)${(s:/:)within}/*/../}
    print "gitdir: $up.git/modules/$within" > $destination/.git
  fi
  local objects=$(git -C $repository rev-parse --path-format=absolute --git-common-dir)/objects
  print $objects > $git_directory/objects/info/alternates
  git -C $destination read-tree $commit && git -C $destination update-ref --no-deref HEAD $commit ||
    fail "pointing the repository at $destination at $commit"
}

# extract writes the tree of <repository> at <commit> into <destination>, makes it a repository at that
# commit, then does the same for each submodule at the commit that tree records. A submodule commit the
# local clone does not have stops the benchmark rather than leaving a hole in the program.
extract() {
  local repository=$1 commit=$2 destination=$3
  mkdir -p $destination
  git -C $repository archive $commit | tar -x -C $destination || fail "extracting $repository at $commit"
  make_repository $repository $commit $destination
  git -C $repository ls-tree -r $commit | awk '$2 == "commit" { print $3 "\t" $4 }' |
    while IFS=$'\t' read -r submodule_commit submodule_path; do
      git -C $repository/$submodule_path cat-file -e "$submodule_commit^{commit}" 2> /dev/null ||
        fail "$repository/$submodule_path does not have $submodule_commit, which $commit records"
      print -u2 "  submodule $submodule_path at ${submodule_commit[1,12]}"
      local within=${destination#$copy}
      print "${within#/}${within:+/}$submodule_path at $submodule_commit" >> $copy/bench-submodules
      extract $repository/$submodule_path $submodule_commit $destination/$submodule_path
    done
}

# tree_hash is one hash over every file in the copy, the caches and linked node_modules aside, so a
# copy that moved by a byte is caught. The git directory is left out but for the excludes the format walk
# applies: git rewrites its own index whenever a command refreshes it, which says nothing about the tree.
tree_hash() {
  (cd $copy && find . -type f -not -path './.cache/*' -not -path './bench-ready' \
    -not \( -path './.git/*' -not -path './.git/info/exclude' \) -print0 | sort -z |
    xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')
}

# prepare_copy sets project_commit, copy and expected_tree, extracting the copy if this commit has none, and
# running the project's generator in it when the sourcing script set `generate` (run_generator).
prepare_copy() {
  project=${project:A}
  [[ -d $project/.git || -f $project/.git ]] || fail "$project is not a git checkout"
  project_commit=$(git -C $project rev-parse HEAD) || fail "cannot read $project's HEAD"
  # The layout is in the name, so a copy extracted before it was a repository is never reused, and so is the
  # generator, so a copy made with one is never taken for a copy made without it, or with another.
  local layout=git
  [[ -n ${generate:-} ]] && layout=git-generated-$(print -rn -- $generate | shasum -a 256 | cut -c1-12)
  copy=$work/${project:t}-${project_commit[1,12]}-$layout
  if [[ -f $copy/bench-ready ]]; then
    print -u2 "reusing the copy at $copy"
    [[ $(tree_hash) == $(< $copy/bench-ready) ]] ||
      fail "the copy at $copy no longer matches what was extracted; remove it and run again"
  else
    [[ -e $copy ]] && fail "$copy exists but was never finished; remove it and run again"
    print -u2 "extracting ${project:t} at ${project_commit[1,12]} into $copy"
    extract $project $project_commit $copy
    # The root is a repository, as the project's checkout is, with the same local excludes.
    mkdir -p $copy/.git/info
    local exclude=$(git -C $project rev-parse --path-format=absolute --git-path info/exclude)
    [[ -f $exclude ]] && cp $exclude $copy/.git/info/exclude
    # Every package in the copy uses the project's installed dependencies, linked rather than copied.
    (cd $copy && find . -name package.json -not -path '*/node_modules/*') | while read -r manifest; do
      local package=${manifest:h}
      [[ -d $project/$package/node_modules ]] && ln -s $project/$package/node_modules $copy/$package/node_modules
    done
    # Each index learns its files' stats once, here, as a checkout's has them, so the first git command a
    # measured run makes does not hash the whole tree to find out that nothing changed. Then each repository
    # must show no change against its commit, or the copy is not the commits it is named by.
    local repositories=($copy) submodule repository
    [[ -f $copy/bench-submodules ]] &&
      for submodule in ${(f)"$(< $copy/bench-submodules)"}; do repositories+=($copy/${submodule%% at *}); done
    for repository in $repositories; do
      git -C $repository update-index -q --refresh > /dev/null
      git -C $repository diff --quiet || fail "$repository differs from the commit it was extracted at"
    done
    [[ -n ${generate:-} ]] && run_generator $repositories
    tree_hash > $copy/bench-ready
  fi
  expected_tree=$(< $copy/bench-ready)
}

# run_generator runs the project's own generator (`generate`) in the copy, after the copy is proven to be its
# commits and before its tree is hashed, so the hash covers what the generator wrote (#e2pe2xp). A checkout
# holds what its project's generators wrote, and a copy without it checks less: ahra's lacked the route types
# Next's dev server writes, so its runs type-checked with 161 errors, held every fix and ran no rule. The copy
# is then its commits plus what the commits' own generator wrote, both fixed by the commits.
#
# The generator may change tracked files, as it does in any checkout (`next typegen` rewrites next-env.d.ts),
# and every one it changed is kept in bench-generated with the command, and printed in the header, so nothing
# hides in the hash. It runs where the network is denied, and a generator that exits nonzero, network refused
# or not, refuses the copy. A generator that sends telemetry is told not to in the command, as
# `NEXT_TELEMETRY_DISABLED=1 next typegen`.
run_generator() {
  local profile=$work/bench-no-network.sb repository changed
  print -l '(version 1)' '(allow default)' '(deny network*)' '(allow network* (local unix))' > $profile
  print -u2 "generating in the copy, with no network: $generate"
  (cd $copy && sandbox-exec -f $profile zsh -c $generate) >&2 ||
    fail "the generator failed in $copy, so the copy is refused: $generate"
  print -r -- $generate > $copy/bench-generated
  for repository in $@; do
    for changed in ${(f)"$(git -C $repository diff --name-only)"}; do
      print -r -- ${${repository#$copy}#/}${${repository#$copy}:+/}$changed >> $copy/bench-generated
    done
  done
}

# pin_engine sets cohere_version, engine_source and engine, a copy of the engine in <logs>, and the Swift
# engine every run uses (pin_swift_engine). Asking the launcher for its version is what makes it build the
# engine for the current commit, so the newest engine in its binary cache is that one, and the two versions
# agreeing proves it. They are compared on cohere's own lines and on the Swift engine's provenance line,
# which the same binary prints the same, and not on the lines that say where the Swift engine came from,
# which differ by design: the launcher resolved it, and the pinned engine is told it.
#
# Another session's launcher can build a newer commit between the two reads, so a mismatch is asked again,
# three times, before it is refused.
pin_engine() {
  local logs=$1 attempt
  whence -p $cohere > /dev/null || [[ -x $cohere ]] || fail "no cohere at $cohere"
  local launcher=$(whence -p $cohere || print ${cohere:A})
  engine=$logs/cohere-engine
  for attempt in 1 2 3; do
    unset COHERE_SWIFT_ENGINE
    cohere_version=$(cd $copy && $cohere --version 2>&1) || fail "cohere --version failed: $cohere_version"
    if [[ ${launcher:A:t} == cohere-dispatch ]]; then
      local newest=(${launcher:A:h}/cohere-darwin-*(N.om[1]))
      (( ${#newest} == 1 )) || fail "the launcher's binary cache at ${launcher:A:h} holds no engine"
      engine_source=$newest[1]
    else
      engine_source=${launcher:A}
    fi
    cp $engine_source $engine
    pin_swift_engine $logs $launcher
    [[ $(cd $copy && $engine --version 2>&1 | engine_identity) == $(print -- "$cohere_version" | engine_identity) ]] &&
      return 0
    print -u2 "the newest engine is not the build the launcher reports, asking again"
    sleep 5
  done
  fail "the engine at $engine_source is not the build the launcher reports; run again once nothing is building"
}

# engine_identity is a `--version` output without the lines that say where the Swift engine came from.
engine_identity() { grep -v -e '^  the Swift engine was built' -e '^cohere: running the Swift engine'; }

# pin_swift_engine sets swift_engine, swift_source and swift_commit, and exports COHERE_SWIFT_ENGINE, so every
# run of a Swift project uses one Swift engine, the one built from the cohere engine's own commit, and no run
# builds one inside its timing (#ywx8zgd). Copied out of the cohere tree, the engine cannot find the checkout
# its Swift engine is built from: on a Swift-only project it refused to run at all, and on cohere's root it
# found the copy and built one there, inside the first timed run.
#
# Through the launcher, the Swift engine is the one dispatch.ResolveSwiftEngine names for the checkout the
# launcher lives in and the engine's full commit, asked through tools/swift_engine, so it is the very file the
# engine would have resolved. A packaged cohere ships its Swift engine beside it. An engine built from a
# modified tree has no commit to build a Swift engine from, so none is pinned, the header says so, and a Swift
# project's run resolves its own.
pin_swift_engine() {
  local logs=$1 launcher=$2
  swift_engine=$logs/cohere-swift
  swift_source= swift_commit=
  if [[ ${launcher:A:t} == cohere-dispatch ]]; then
    local module=${launcher:A:h:h:h:h}
    [[ -f $module/go.mod ]] || fail "the launcher at ${launcher:A} is not inside a cohere checkout"
    print -- "$cohere_version" | grep -q '^  note: .*with nothing uncommitted in it' || return 0
    local commit=$(print -- "$cohere_version" | awk '$1 == "commit:" { print $2; exit }')
    local contract=$(print -- "$cohere_version" | awk '$1 == "swift" && $2 == "contract:" { print $3; exit }')
    swift_commit=$(git -C $module rev-parse --verify "$commit^{commit}") ||
      fail "the engine's commit $commit is not in $module"
    (cd ${bench_directory:h} && go build -o $logs/swift_engine ./internal/bench_results/tools/swift_engine) ||
      fail "could not build the Swift engine resolver"
    swift_source=$($logs/swift_engine $module $swift_commit $contract) ||
      fail "could not resolve the Swift engine built from $swift_commit"
  elif [[ -f ${engine_source:h}/cohere-swift ]]; then
    swift_source=${engine_source:h}/cohere-swift
    swift_commit="the package's own"
  else
    return 0
  fi
  cp $swift_source $swift_engine
  export COHERE_SWIFT_ENGINE=$swift_engine
}

# swift_engine_named_otherwise prints the first Swift engine a run's <log> says it ran that is not the pinned
# one, or that it resolved on its own, so a round never measures two builds. Nothing when the run named the
# pinned engine, or ran no Swift.
swift_engine_named_otherwise() {
  local log=$1
  if [[ -n ${COHERE_SWIFT_ENGINE:-} ]]; then
    awk -v pinned=$swift_engine '
      /^cohere: running the Swift engine / && $6 != pinned { print $6; exit }
      /^  the Swift engine was built from / { print "one it resolved itself"; exit }' $log
  fi
}

# print_pinned_header prints the commits, the machine and the engine every report starts with.
print_pinned_header() {
  print "# project  ${project:t} at $project_commit"
  [[ -f $copy/bench-submodules ]] && sed 's/^/#   submodule /' $copy/bench-submodules
  if [[ -f $copy/bench-generated ]]; then
    print "# generate $(head -1 $copy/bench-generated)"
    tail -n +2 $copy/bench-generated | sed 's/^/#   changed /'
  fi
  print "# machine  $(sysctl -n hw.model), $(sysctl -n machdep.cpu.brand_string), $(sysctl -n hw.ncpu) cores," \
    "$(( $(sysctl -n hw.memsize) / 1073741824 )) GB, macOS $(sw_vers -productVersion)"
  print "# cohere   $engine_source"
  print -- "$cohere_version" | sed 's/^/#   /'
  if [[ -n ${COHERE_SWIFT_ENGINE:-} ]]; then
    print "# swift    $swift_source, built from commit $swift_commit, pinned as $swift_engine"
  else
    print "# swift    not pinned: the engine was built from a modified tree, so a Swift project resolves its own"
  fi
  print "# copy     $copy"
}

# run_timed runs a command in the copy and sets verdict_seconds, settled_seconds and exit_code. It does
# what the cohere launcher does, hands the command a descriptor to write its exit code to and takes the
# verdict when that byte arrives, and then waits for the command's whole process group, so a cache
# write still running cannot overlap the next run. A command that never writes the byte has its
# verdict when it exits, so for any tool but cohere the two times are the same run to its end.
run_timed() {
  local log=$1 timing=$1.timing; shift
  (cd $copy && perl -MPOSIX -MTime::HiRes=time -e '
    my $timing = shift;
    pipe(my $reader, my $writer) or die "pipe: $!";
    my $started = time;
    my $engine = fork() // die "fork: $!";
    if ($engine == 0) {
      setpgrp(0, 0);
      POSIX::dup2(fileno($writer), 3) or die "dup2: $!";
      $ENV{COHERE_VERDICT_FD} = 3;
      exec(@ARGV) or die "exec: $!";
    }
    close $writer;
    my $exit_code;
    $exit_code = ord($verdict) if sysread($reader, my $verdict, 1) == 1;
    my $answered = time;
    waitpid($engine, 0);
    $exit_code //= $? >> 8;
    select(undef, undef, undef, 0.02) while kill(0, -$engine);
    my $settled = time;
    open(my $out, ">", $timing) or die "$timing: $!";
    printf $out "%.3f %.3f %d\n", $answered - $started, $settled - $started, $exit_code;
  ' $timing "$@") < /dev/null > $log 2>&1 || fail "could not run $1, see $log"
  read -r verdict_seconds settled_seconds exit_code < $timing
}
