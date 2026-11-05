package app

import (
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

const outputText = pipeline.FormatText

type pipePipeline struct {
	pipeline    pipeline.Pipeline
	displayJSON bool
}

func wrapPipePipeline(parsed pipeline.Pipeline) pipePipeline {
	return pipePipeline{
		pipeline:    parsed,
		displayJSON: parsed.DisplayJSON(),
	}
}

func (p pipePipeline) shellOutput(text string, ok bool) (string, error) {
	if text != "" {
		text = render.LayoutText(text, render.Presentation{})
	}
	out, err := p.pipeline.Apply(text)
	if err != nil {
		return "", err
	}
	if p.displayJSON {
		return pipeline.JSON(out, ok)
	}
	if out == "" {
		return "", nil
	}
	return render.LayoutText(out, terminalPresentation()), nil
}
