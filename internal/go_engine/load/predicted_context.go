package load

import "crypto/sha256"

// PredictedContext is a go command's build context as the system cache keeps it, predicted from the host's go env and
// the command's own environment before its go env forks (PredictContext, #sfjj2rh): the store, the key, and what the
// store held under it then. The bytes are read once, there, so a decision made on a hit, that an extra build forks
// nothing after its go env, never loses them to a trim or another run's write before the build reads them.
type PredictedContext struct {
	store  ListingStore
	key    [sha256.Size]byte
	kept   []byte
	stored bool
}

// PredictContext is the build context of go commands under environment, the caller's entries, as store, the system
// cache's, keeps it under the key predicted from host's answer, the run's host's go env (predictedContextKey), read
// now: what an early start reads before an extra build's go env forks, or, for the host's own, host's own environment.
// It is nil when nothing is kept: no store, under `--no-system-cache` or with no system cache, a run under `--timing`,
// whose children's CPU must be of its own commands alone, no host, or a key contextKey keeps none under.
func PredictContext(store ListingStore, host *GoEnvironment, environment []string, timed bool) *PredictedContext {
	if store == nil || host == nil || timed {
		return nil
	}
	key, keeps := predictedContextKey(host, environment)
	if !keeps {
		return nil
	}
	kept, stored := store.Kept(key)
	return &PredictedContext{store: store, key: key, kept: kept, stored: stored}
}

// Kept reports whether the store held the predicted build context, so the command will not run unless the answer's
// own key contradicts the prediction.
func (predicted *PredictedContext) Kept() bool {
	return predicted != nil && predicted.stored
}

// read is what the store held under the predicted key: the bytes read with the prediction, or, when it held none
// then, what it holds now, since another run may have kept it since.
func (predicted *PredictedContext) read() ([]byte, bool) {
	if predicted.stored {
		return predicted.kept, true
	}
	return predicted.store.Kept(predicted.key)
}
