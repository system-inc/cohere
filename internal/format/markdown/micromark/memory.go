package micromark

import "github.com/system-inc/cohere/internal/format/arena"

// Memory is what a parse takes its tokens, attempts and tokenizers from, kept for the next parse once the
// events it returned are done (#93dpede, #vbjv3d6). A Memory belongs to one parse at a time.
//
// Attempts and tokenizers are the tokenizer's small objects: an attempt's three states and a tokenizer's
// effects are closures over the struct, so a fresh struct costs a closure per state as well. Here each
// slot binds its closures once, when it is first made, and every later parse that takes the slot reuses
// them. Within one parse no slot is handed out twice, exactly as fresh structs were.
type Memory struct {
	Tokens arena.Arena[Token]

	attempts       []*attempt
	attemptsUsed   int
	tokenizers     []*TokenizeContext
	tokenizersUsed int
}

// NewMemory is an empty Memory whose released tokens hold a poison no parse produces.
func NewMemory() *Memory {
	released := Point{Line: 1 << 30, Column: 1 << 30, Offset: 1 << 30}
	return &Memory{Tokens: arena.Arena[Token]{Poison: Token{Type: "released", Start: released, End: released}}}
}

// Reset releases everything the last parse took. Nothing may read its events, tokens or tokenizers after.
//
// A released attempt or tokenizer is zeroed but for its bound closures, so one read after its parse
// dereferences nil and stops the format rather than reading another file's state.
func (memory *Memory) Reset() {
	memory.Tokens.Reset()
	for _, run := range memory.attempts[:memory.attemptsUsed] {
		run.release()
	}
	memory.attemptsUsed = 0
	for _, context := range memory.tokenizers[:memory.tokenizersUsed] {
		context.release()
	}
	memory.tokenizersUsed = 0
}

// tokens is where a parse's tokens come from, nil to allocate each.
func (memory *Memory) tokens() *arena.Arena[Token] {
	if memory == nil {
		return nil
	}
	return &memory.Tokens
}

// attempt is an attempt for one call of attempt, check or interrupt, its states bound.
func (memory *Memory) attempt() *attempt {
	if memory == nil {
		return newAttempt()
	}
	if memory.attemptsUsed == len(memory.attempts) {
		memory.attempts = append(memory.attempts, newAttempt())
	}
	run := memory.attempts[memory.attemptsUsed]
	memory.attemptsUsed++
	return run
}

// tokenizer is a tokenizer with its effects bound and every other field zero.
func (memory *Memory) tokenizer() *TokenizeContext {
	if memory == nil {
		return newTokenizeContext()
	}
	if memory.tokenizersUsed == len(memory.tokenizers) {
		memory.tokenizers = append(memory.tokenizers, newTokenizeContext())
	}
	context := memory.tokenizers[memory.tokenizersUsed]
	memory.tokenizersUsed++
	return context
}
