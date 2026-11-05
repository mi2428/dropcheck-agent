package app

import (
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

func shellOutput(p pipeline.Pipeline, text string, ok bool) (string, error) {
	if text != "" {
		text = render.LayoutText(text, render.Presentation{})
	}
	out, err := p.Apply(text)
	if err != nil {
		return "", err
	}
	if p.DisplayJSON() {
		return pipeline.JSON(out, ok)
	}
	if out == "" {
		return "", nil
	}
	return render.LayoutText(out, terminalPresentation()), nil
}
