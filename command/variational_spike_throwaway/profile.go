package main

import (
	"fmt"
	"go/token"
	"os"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"sort"

	"github.com/system-inc/cohere/internal/cpu_clock"
	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/cohere/internal/variational_types/types"
)

// profileRate is the CPU profile's samples a second, above pprof's 100 so a check of tenths of a second leaves enough.
const profileRate = 500

// hostPlainProfileSuffix ends the host plain check's profile's name, beside the variational check's.
const hostPlainProfileSuffix = ".host-plain"

// The runtime's counts a repeat reads around itself: the CPU its collector spent, the part of it idle mark workers
// spent on processors nothing else wanted, and the bytes it allocated.
const (
	collectorMetric     = "/cpu/classes/gc/total:cpu-seconds"
	collectorIdleMetric = "/cpu/classes/gc/mark/idle:cpu-seconds"
	allocationMetric    = "/gc/heap/allocs:bytes"
)

// profileRepeat is one repeat of one check over every package: its time, the CPU of the thread the check ran on
// alone, the collector's CPU in it and the idle mark workers' part of that, and what it allocated.
type profileRepeat struct {
	Check phaseTiming `json:"check"`
	// ThreadCPUSeconds is the check's own thread's CPU: its work, the collector assists it was charged and the
	// runtime work it did on its own stack, but none of the background mark workers' on other threads.
	ThreadCPUSeconds        float64 `json:"threadCpuSeconds"`
	CollectorCPUSeconds     float64 `json:"collectorCpuSeconds"`
	CollectorIdleCPUSeconds float64 `json:"collectorIdleCpuSeconds"`
	AllocatedBytes          uint64  `json:"allocatedBytes"`
}

// profileReport is the repeats of both checks under the profiler, and where their profiles went.
type profileReport struct {
	Repeats            int             `json:"repeats"`
	RateHertz          int             `json:"rateHertz"`
	VariationalProfile string          `json:"variationalProfile"`
	HostPlainProfile   string          `json:"hostPlainProfile"`
	Variational        []profileRepeat `json:"variational"`
	HostPlain          []profileRepeat `json:"hostPlain"`
}

// profileChecks runs the variational check repeats more times over ours, each a fresh check through importer, which the
// timed run left warm, under a CPU profile written to profileFile; then the host's plain check as many times, with its
// Types map as the baseline records, through hostImporter, under a profile written beside it.
func profileChecks(
	fileSet *token.FileSet,
	ordered []*ourPackage,
	ours map[string]*ourPackage,
	importer *merged.Importer,
	all types.Mask,
	host *listedBuild,
	hostImporter types.Importer,
	sizes types.Sizes,
	repeats int,
	profileFile string,
) (*profileReport, error) {
	profiled := &profileReport{
		Repeats:            repeats,
		RateHertz:          profileRate,
		VariationalProfile: profileFile,
		HostPlainProfile:   profileFile + hostPlainProfileSuffix,
	}
	variationalError := underProfile(profiled.VariationalProfile, func() {
		for range repeats {
			profiled.Variational = append(profiled.Variational, repeatOnce(func() {
				checkVariationally(fileSet, ordered, ours, importer, all, sizes, unmeasured)
			}))
		}
	})
	if variationalError != nil {
		return nil, variationalError
	}
	hostError := underProfile(profiled.HostPlainProfile, func() {
		for range repeats {
			profiled.HostPlain = append(profiled.HostPlain, repeatOnce(func() {
				checkPlainly(fileSet, ordered, host, hostImporter, 0, sizes, unmeasured)
			}))
		}
	})
	if hostError != nil {
		return nil, hostError
	}
	return profiled, nil
}

// underProfile runs work under a CPU profile written to file.
func underProfile(file string, work func()) error {
	output, createError := os.Create(file)
	if createError != nil {
		return createError
	}
	// The collector's leftovers from the run before would land in this profile.
	runtime.GC()
	// StartCPUProfile sets pprof's own 100 a second, which a rate set just before it overrides, with a warning.
	runtime.SetCPUProfileRate(profileRate)
	if startError := pprof.StartCPUProfile(output); startError != nil {
		// Ignored: the profile never started, and its file is the error's to explain.
		_ = output.Close()
		return fmt.Errorf("starting the profile %s: %w", file, startError)
	}
	work()
	pprof.StopCPUProfile()
	return output.Close()
}

// repeatOnce runs one repeat, measured as a whole, its imports with it (the importer is warm, so they are cache hits),
// on a goroutine held to its thread so the thread's own CPU clock is the check's, and reads the collector's CPU and
// the bytes allocated around it.
func repeatOnce(repeat func()) profileRepeat {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	samples := []metrics.Sample{{Name: collectorMetric}, {Name: collectorIdleMetric}, {Name: allocationMetric}}
	metrics.Read(samples)
	collectorBefore, idleBefore := samples[0].Value.Float64(), samples[1].Value.Float64()
	allocatedBefore := samples[2].Value.Uint64()
	threadBefore, threadKnown := cpu_clock.Thread()
	check := measure(repeat)
	threadAfter, threadKnownAfter := cpu_clock.Thread()
	metrics.Read(samples)
	threadCPU := 0.0
	if threadKnown && threadKnownAfter {
		threadCPU = (threadAfter - threadBefore).Seconds()
	}
	return profileRepeat{
		Check:                   check,
		ThreadCPUSeconds:        threadCPU,
		CollectorCPUSeconds:     samples[0].Value.Float64() - collectorBefore,
		CollectorIdleCPUSeconds: samples[1].Value.Float64() - idleBefore,
		AllocatedBytes:          samples[2].Value.Uint64() - allocatedBefore,
	}
}

// repeatSummary is repeats' process CPU and their check thread's, each median, least and mean, the collector's CPU over them all and its idle mark workers'
// part, and the median bytes allocated.
type repeatSummary struct {
	medianCPU            float64
	leastCPU             float64
	meanCPU              float64
	medianThreadCPU      float64
	leastThreadCPU       float64
	meanThreadCPU        float64
	collectorCPU         float64
	collectorIdleCPU     float64
	medianAllocatedBytes float64
}

// summarize is repeats' summary.
func summarize(repeats []profileRepeat) repeatSummary {
	if len(repeats) == 0 {
		return repeatSummary{}
	}
	summary := repeatSummary{}
	cpu := make([]float64, len(repeats))
	thread := make([]float64, len(repeats))
	allocated := make([]float64, len(repeats))
	for index, repeat := range repeats {
		cpu[index] = repeat.Check.CPUSeconds()
		thread[index] = repeat.ThreadCPUSeconds
		summary.meanThreadCPU += thread[index] / float64(len(repeats))
		allocated[index] = float64(repeat.AllocatedBytes)
		summary.meanCPU += cpu[index] / float64(len(repeats))
		summary.collectorCPU += repeat.CollectorCPUSeconds
		summary.collectorIdleCPU += repeat.CollectorIdleCPUSeconds
	}
	summary.leastCPU = least(cpu)
	summary.medianCPU = median(cpu)
	summary.leastThreadCPU = least(thread)
	summary.medianThreadCPU = median(thread)
	summary.medianAllocatedBytes = median(allocated)
	return summary
}

// median is values' median, sorting them.
//
// Mutates: values is sorted.
func median(values []float64) float64 {
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

// least is values' smallest.
func least(values []float64) float64 {
	smallest := values[0]
	for _, value := range values[1:] {
		smallest = min(smallest, value)
	}
	return smallest
}
