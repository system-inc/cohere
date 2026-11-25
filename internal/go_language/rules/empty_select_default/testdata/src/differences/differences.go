// Package differences holds the loops where the port's test of syntax could most plausibly part from the pattern, and
// where the deleting fix's range matters: a select under a label inside a block, a default case spread over lines
// with a comment beside it, and one on a single line. The port and the original find the same here, fixes included,
// so the fixture records no difference.
package differences

func shapes(channel chan int, done chan struct{}) {
	for {
		{
		label:
			select {
			case <-done:
				break label
			default:
			}
		}
	}
	for {
		select {
		case <-channel:
		default: // Nothing to do.
		}
	}
	for {
		select {
		default:
		}
	}
}
