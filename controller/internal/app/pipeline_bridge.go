package app

import (
	"dropcheck/controller/internal/pipeline"
	"dropcheck/controller/internal/render"
)

type outputFormat = pipeline.Format

const (
	outputText outputFormat = pipeline.FormatText
	outputJSON outputFormat = pipeline.FormatJSON
)

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

func (p pipePipeline) apply(text string) (string, error) {
	return p.pipeline.Apply(text)
}

func (p pipePipeline) shellOutput(text string, ok bool) (string, error) {
	if text != "" {
		text = render.LayoutText(text, render.Presentation{})
	}
	out, err := p.apply(text)
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
