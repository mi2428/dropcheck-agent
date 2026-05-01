package app

import "dropcheck/controller/internal/pipeline"

type outputFormat = pipeline.Format

const (
	outputText outputFormat = pipeline.FormatText
	outputJSON outputFormat = pipeline.FormatJSON
)

type pipePipeline struct {
	pipeline    pipeline.Pipeline
	displayJSON bool
	stages      []pipeStage
}

type pipeStage struct{}

func wrapPipePipeline(parsed pipeline.Pipeline) pipePipeline {
	return pipePipeline{
		pipeline:    parsed,
		displayJSON: parsed.DisplayJSON(),
		stages:      make([]pipeStage, parsed.StageCount()),
	}
}

func (p pipePipeline) format(defaultFormat outputFormat) outputFormat {
	return p.pipeline.Format(defaultFormat)
}

func (p pipePipeline) apply(text string) (string, error) {
	return p.pipeline.Apply(text)
}
