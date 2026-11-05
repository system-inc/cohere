package oracletest

import (
	"fmt"
	"sync"

	"github.com/system-inc/cohere/internal/format/formatoptions"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// Engine is a prettier.Engine's Format, answered from a golden.
type Engine struct {
	golden  *Golden
	options formatoptions.Options
	once    sync.Once
	engine  *prettier.Engine
	err     error
}

// Format is the answer prettier.Engine.Format gives for fileName and text under the engine's options.
func (engine *Engine) Format(fileName string, text string) (string, error) {
	return engine.golden.Answer(Key(
		"engine",
		fmt.Sprintf("%+v", engine.options),
		fileName,
		text,
	), func() (string, error) {
		engine.once.Do(func() { engine.engine, engine.err = prettier.New(engine.options) })
		if engine.err != nil {
			return "", engine.err
		}
		return engine.engine.Format(fileName, text)
	})
}
