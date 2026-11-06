package oracle_testing

import (
	"fmt"
	"sync"

	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// Engine is a prettier.Engine's Format, answered from a golden.
type Engine struct {
	golden      *Golden
	options     format_options.Options
	once        sync.Once
	engine      *prettier.Engine
	engineError error
}

// Format is the answer prettier.Engine.Format gives for fileName and text under the engine's options.
func (engine *Engine) Format(fileName string, text string) (string, error) {
	return engine.golden.Answer(Key(
		"engine",
		fmt.Sprintf("%+v", engine.options),
		fileName,
		text,
	), func() (string, error) {
		engine.once.Do(func() { engine.engine, engine.engineError = prettier.New(engine.options) })
		if engine.engineError != nil {
			return "", engine.engineError
		}
		return engine.engine.Format(fileName, text)
	})
}
