package duplicate_functions

import (
	"cmp"
	"slices"
)

// Floor is the size from which a group is reported, in each engine's own instructions. Measured on #6rj7jw7: below
// it most matches were delegations, upstream ports mirrored on purpose and decorator arrows; from it up nearly every
// match was a helper that belongs in a library (@system_cohere's ruling, 2026-11-08).
const Floor = 11

// RuleName is the name a suppression gives to keep a function in its shape, spelled as each engine's house rules are
// (policy/RuleNaming.json). It is a rule's name before it is a rule's: the gate on #faxsxch turns it into one, and a
// suppression written for the report today is the one the gate reads.
const (
	TypeScriptRuleName = "nexus/consistency-no-duplicate-function"
	GoRuleName         = "cohere-go/consistency-no-duplicate-function"
)

// Report is one run's duplicates: what was grouped, and every count a reader needs to tell a report that read
// nothing from one that found nothing.
type Report struct {
	// Ran is false when the report was asked for and could not run, NotRun saying why.
	Ran    bool   `json:"ran"`
	NotRun string `json:"notRun,omitempty"`

	Floor int `json:"floor"`

	// Files is the files whose functions were looked at, and FilesIgnored those the settings leave out.
	Files        int `json:"files"`
	FilesIgnored int `json:"filesIgnored"`

	// Functions is every function looked at: Hashed, plus each Refused, plus each NotRead.
	Functions int `json:"functions"`
	Hashed    int `json:"hashed"`

	// Groups are the groups at the floor with at least one member no suppression keeps, largest first.
	Groups []Group `json:"groups"`

	// Withheld is the groups at the floor whose every member a suppression keeps, and BelowFloor the groups under it.
	Withheld   int `json:"withheld"`
	BelowFloor int `json:"belowFloor"`

	Refused []Refusal             `json:"refused"`
	NotRead map[NotReadReason]int `json:"notRead"`

	// Crashes are what the hash could not read at all.
	Crashes []Crash `json:"crashes,omitempty"`

	// DeadSuppressions are the suppressions naming this report that keep no member of any group at the floor. Listed,
	// never findings, while this is a report (#jfvhbj7).
	DeadSuppressions []Place `json:"deadSuppressions"`

	ElapsedMilliseconds int      `json:"elapsedMilliseconds"`
	Cache               CacheUse `json:"cache"`
}

// Assemble groups the hashed functions by digest into report's groups, and returns the place of every member of a
// group at the floor, whether reported or withheld, which is what a suppression must keep to be alive.
//
// A group is withheld only when every member is kept. A suppression says one function keeps its shape on purpose, a
// port of upstream say, and that excuses no copy of it: a group of a kept port and a function of ours is still one
// idea written twice, and ours should call the port.
func (report *Report) Assemble(functions []Function) map[Place]bool {
	report.Floor = Floor
	report.Hashed = len(functions)
	report.Functions += len(functions)
	byDigest := map[Digest][]Function{}
	for _, function := range functions {
		byDigest[function.Digest] = append(byDigest[function.Digest], function)
	}
	atFloor := map[Place]bool{}
	report.Groups = []Group{}
	for digest, members := range byDigest {
		if len(members) < 2 {
			continue
		}
		slices.SortFunc(members, func(left Function, right Function) int {
			return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Line, right.Line),
				cmp.Compare(left.Name, right.Name))
		})
		group := Group{Digest: digest, Size: members[0].Size, Members: members}
		everyMemberKept := true
		for _, member := range members {
			group.Size = min(group.Size, member.Size)
			if member.Kept == "" {
				everyMemberKept = false
			}
		}
		if group.Size < Floor {
			report.BelowFloor++
			continue
		}
		for _, member := range members {
			atFloor[Place{File: member.File, Line: member.Line}] = true
		}
		if everyMemberKept {
			report.Withheld++
			continue
		}
		report.Groups = append(report.Groups, group)
	}
	slices.SortFunc(report.Groups, func(left Group, right Group) int {
		return cmp.Or(
			cmp.Compare(right.Size, left.Size),
			cmp.Compare(len(right.Members), len(left.Members)),
			cmp.Compare(left.Members[0].File, right.Members[0].File),
			cmp.Compare(left.Members[0].Line, right.Members[0].Line),
		)
	})
	return atFloor
}

// Refuse records one refused function.
func (report *Report) Refuse(refusal Refusal) {
	report.Refused = append(report.Refused, refusal)
	report.Functions++
}

// Skip records count functions never read, for reason.
func (report *Report) Skip(reason NotReadReason, count int) {
	if count == 0 {
		return
	}
	if report.NotRead == nil {
		report.NotRead = map[NotReadReason]int{}
	}
	report.NotRead[reason] += count
	report.Functions += count
}

// SortRefused orders the refusals by file, line and name, so a report reads the same every run.
func (report *Report) SortRefused() {
	slices.SortFunc(report.Refused, func(left Refusal, right Refusal) int {
		return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Name, right.Name))
	})
	slices.SortFunc(report.DeadSuppressions, func(left Place, right Place) int {
		return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Line, right.Line))
	})
}
