package dispatch

import (
	"go/token"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// defaultShardBytes is how much source a dispatcher hands one shard of a package whose file-local listeners walk on
// several cores (#0jnq91n). A package splits only when it holds at least two shards' worth, so only a package large
// enough to end a cold run splits, and into few shards: each shard costs CPU the walk never had, in cores contending
// for what the package shares, such as its file set's lock. At cohere's root two test variants split:
// internal/lint/rules/core's, 14.3 MB of source, into 4 shards, and internal/lint/rules/typescript's, 5.8 MB, into 2,
// which at 4 MiB sat under the split and ended the run's half-idle tail (#3ddm7fm). 256 KB shards, splitting 24
// packages, bought less wall for five times the CPU, and shards small enough to split react's 5.2 MB test variant too
// bought nothing more. Measured on cohere's root, cold, bare and interleaved: see the commit that set it.
const defaultShardBytes = 11 << 18

// shardOfFile is the shard of each of pass's files, keyed by its token file, cut into shards runs of consecutive files
// holding about as much source each, so every file is one shard's and each shard's files keep the pass's order.
func shardOfFile(pass *analysis.Pass, shards int) map[*token.File]int {
	files := make([]*token.File, 0, len(pass.Files))
	total := 0
	for _, file := range pass.Files {
		if tokenFile := pass.Fset.File(file.Pos()); tokenFile != nil {
			files = append(files, tokenFile)
			total += tokenFile.Size()
		}
	}
	shardOf := make(map[*token.File]int, len(files))
	seen := 0
	for _, tokenFile := range files {
		// The shard the file's first byte falls in, of shards equal cuts of the package's source.
		shardOf[tokenFile] = min(seen*shards/max(total, 1), shards-1)
		seen += tokenFile.Size()
	}
	return shardOf
}

// walkConcurrently runs each walk on a goroutine of its own and returns once all have. Under `--timing` each walk's
// CPU is measured on its own goroutine and added to the dispatcher's row, which the checker's measurement of the
// dispatcher's action, on the goroutine waiting here, cannot see; finish then takes the listeners' calls out of it.
func walkConcurrently(walks []shardWalk) {
	var group sync.WaitGroup
	for _, each := range walks {
		group.Go(func() {
			if each.started.timing == nil {
				each.walk()
				return
			}
			each.started.timing.Shared(each.started.dispatcher, each.started.timing.Measure(each.walk))
		})
	}
	group.Wait()
}

// shardWalk is one walk of a split package: the runs it calls, and the walk over its share.
type shardWalk struct {
	started *startedRuns
	walk    func()
}
