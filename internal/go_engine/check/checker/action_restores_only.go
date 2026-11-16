package checker

// restoresOnly is whether act will only restore its package's analysis, copying what the findings cache or the host's
// run holds (Options.Restored), and run no pass: the very test execOnce makes before it calls Restore. Such an action
// takes no slot of the run's pool, since a slot bounds the passes that keep a core busy, and an extra build restores
// about 200,000 actions a run at cohere's root, each of which parked for a slot: on Shackler, letting them run without
// one took 0.17 s and 1.05 CPU-s off an every-build `--no-cache` run, and a quarter of its involuntary context switches
// (#ss4t5g6). An action on a package that is not restored runs its pass, and takes a slot.
func (act *Action) restoresOnly() bool {
	return act.opts.Restored != nil && act.opts.Restored(act.Package)
}
