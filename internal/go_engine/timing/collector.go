// Package timing collects, under `--timing`, what a Go run's CPU went to: each analyzer action's, each dispatched
// listener's and each phase of the load's, by the kernel's thread clock, for the timing record (#3apgjer).
//
// A Collector travels by argument, never as package state: the engine makes one for a run that asked, and hands it to
// the load, to check.Run and through it to each pass, so two runs in one process, as two parallel tests are, never
// bill each other. A nil *Collector is a run that did not ask, and every method on it does nothing, so a caller
// measures without asking first.
package timing

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/system-inc/cohere/internal/cpu_clock"
	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// calibrationReads is how many back-to-back clock reads New times to learn what one costs.
const calibrationReads = 1000

// Collector sums the CPU of the work it is told about. It is safe to use from many goroutines at once.
type Collector struct {
	// unavailable says why the thread clock cannot be read here, empty when it can.
	unavailable string
	// perCall is what one Measure costs in clock reads, learned by New and taken out of every measurement.
	perCall time.Duration
	// calls counts every Measure.
	calls atomic.Int64

	mutex     sync.Mutex
	analyzers costs
	listeners costs
	nested    map[string]time.Duration
	load      loadCost
}

// loadCost is the load's phases as they are added.
type loadCost struct {
	list, listChildren, parse, typeCheck, exportData     time.Duration
	parsedFiles, typeCheckedPackages, exportDataPackages int
}

// New is a collector for one run, which learns what a measurement costs before it measures anything.
func New() *Collector {
	collector := &Collector{
		analyzers: costs{},
		listeners: costs{},
		nested:    map[string]time.Duration{},
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	start, known := cpu_clock.Thread()
	if !known {
		collector.unavailable = "this platform has no per-thread CPU clock"
		return collector
	}
	for range calibrationReads {
		cpu_clock.Thread()
	}
	end, _ := cpu_clock.Thread()
	// Two reads to a measurement, and the loop's own reads bracketed by two more.
	collector.perCall = 2 * (end - start) / (calibrationReads + 1)
	return collector
}

// Measure runs work on a thread held for it and returns the CPU it cost, the measuring taken out, never below 0. On a
// nil collector, or with no clock, it runs work and returns 0. A measurement inside another is fine: the inner work
// is in both, and the caller of the outer one subtracts what it nests (Nested).
func (collector *Collector) Measure(work func()) time.Duration {
	if collector == nil || collector.unavailable != "" {
		work()
		return 0
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	start, _ := cpu_clock.Thread()
	work()
	end, _ := cpu_clock.Thread()
	collector.calls.Add(1)
	return max(end-start-collector.perCall, 0)
}

// Analyzer adds one action's CPU, an analyzer run on one package, under the analyzer's name.
func (collector *Collector) Analyzer(name string, cpu time.Duration) {
	collector.add(func() { collector.analyzers.add(name, cpu, 1) })
}

// Shared adds cpu to the analyzer named name's row without counting a package, for work the analyzer's result does
// for it after its action has finished: the engine's own intermediate representation, which builds its package on the
// first read, inside the action of the dispatcher whose walk reads it first.
func (collector *Collector) Shared(name string, cpu time.Duration) {
	collector.add(func() { collector.analyzers.add(name, cpu, 0) })
}

// Listener adds the CPU of calls into a dispatched rule's listener over one package, under the rule's name.
func (collector *Collector) Listener(rule string, cpu time.Duration, calls int64) {
	collector.add(func() { collector.listeners.add(rule, cpu, calls) })
}

// Nested takes cpu out of the row of the analyzer or dispatched rule named name, for the work calls measurements
// inside its action or its listener's calls billed elsewhere: a dispatcher's calls into its listeners, which are
// their rules' rows, or a rule's questions to the fact resolver, which are the resolver's rows. The clock reads of
// those calls are taken out too, since the outer measurement holds them and no row is to be inflated by the measuring.
func (collector *Collector) Nested(name string, cpu time.Duration, calls int64) {
	collector.add(func() { collector.nested[name] += cpu + time.Duration(calls)*collector.perCall })
}

// List adds the listing's CPU: this process's own, and its go commands' (children).
func (collector *Collector) List(own time.Duration, children time.Duration) {
	collector.add(func() {
		collector.load.list += own
		collector.load.listChildren += children
	})
}

// Parse adds the CPU of reading and parsing files files.
func (collector *Collector) Parse(cpu time.Duration, files int) {
	collector.add(func() {
		collector.load.parse += cpu
		collector.load.parsedFiles += files
	})
}

// TypeCheck adds the CPU of type-checking one package from source.
func (collector *Collector) TypeCheck(cpu time.Duration) {
	collector.add(func() {
		collector.load.typeCheck += cpu
		collector.load.typeCheckedPackages++
	})
}

// ExportData adds the CPU of reading one package's types from export data.
func (collector *Collector) ExportData(cpu time.Duration) {
	collector.add(func() {
		collector.load.exportData += cpu
		collector.load.exportDataPackages++
	})
}

// add runs change under the lock, and nothing on a nil collector.
func (collector *Collector) add(change func()) {
	if collector == nil {
		return
	}
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	change()
}

// Kinds says, for an analyzer name, which rule it is the analyzer of, or else what kind of shared row it is. The
// engine answers it from its registry, which is the one place that knows which analyzer is which rule's.
type Kinds func(analyzer string) (rule string, shared contract.SharedKind)

// Record is the timing record of what was collected. kinds names each analyzer's rule or shared kind: an analyzer
// that is a rule's is added to that rule's row with its listener's calls, and any other is a shared row, less what
// was nested in it. On a nil collector it is the zero record.
func (collector *Collector) Record(kinds Kinds) contract.TimingRecord {
	if collector == nil {
		return contract.TimingRecord{}
	}
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	record := contract.TimingRecord{
		Unavailable:              collector.unavailable,
		MeasuredCalls:            collector.calls.Load(),
		MeasuringCpuMicroseconds: (time.Duration(collector.calls.Load()) * collector.perCall).Microseconds(),
		Load: contract.LoadTiming{
			ListCpuMicroseconds:         collector.load.list.Microseconds(),
			ListChildrenCpuMicroseconds: collector.load.listChildren.Microseconds(),
			ParseCpuMicroseconds:        collector.load.parse.Microseconds(),
			ParsedFiles:                 collector.load.parsedFiles,
			TypeCheckCpuMicroseconds:    collector.load.typeCheck.Microseconds(),
			TypeCheckedPackages:         collector.load.typeCheckedPackages,
			ExportDataCpuMicroseconds:   collector.load.exportData.Microseconds(),
			ExportDataPackages:          collector.load.exportDataPackages,
		},
	}
	if processCpu, known := cpu_clock.Process(); known {
		record.ProcessCpuMicroseconds = processCpu.Microseconds()
	}
	rules := map[string]*contract.RuleTiming{}
	ruleRow := func(name string) *contract.RuleTiming {
		row, known := rules[name]
		if !known {
			row = &contract.RuleTiming{Name: name}
			rules[name] = row
		}
		return row
	}
	for analyzer, spent := range collector.analyzers {
		rule, shared := kinds(analyzer)
		if rule != "" {
			row := ruleRow(rule)
			row.CpuMicroseconds += max(spent.cpu-collector.nested[analyzer], 0).Microseconds()
			row.Packages += int(spent.count)
			continue
		}
		record.Shared = append(record.Shared, contract.SharedTiming{
			Name:            analyzer,
			Kind:            shared,
			CpuMicroseconds: max(spent.cpu-collector.nested[analyzer], 0).Microseconds(),
			Packages:        int(spent.count),
		})
	}
	for rule, spent := range collector.listeners {
		row := ruleRow(rule)
		row.CpuMicroseconds += max(spent.cpu-collector.nested[rule], 0).Microseconds()
		row.Calls += spent.count
	}
	for _, row := range rules {
		record.Rules = append(record.Rules, *row)
	}
	sort.Slice(record.Rules, func(left, right int) bool {
		if record.Rules[left].CpuMicroseconds != record.Rules[right].CpuMicroseconds {
			return record.Rules[left].CpuMicroseconds > record.Rules[right].CpuMicroseconds
		}
		return record.Rules[left].Name < record.Rules[right].Name
	})
	sort.Slice(record.Shared, func(left, right int) bool {
		if record.Shared[left].CpuMicroseconds != record.Shared[right].CpuMicroseconds {
			return record.Shared[left].CpuMicroseconds > record.Shared[right].CpuMicroseconds
		}
		return record.Shared[left].Name < record.Shared[right].Name
	})
	return record
}
