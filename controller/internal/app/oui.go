package app

import (
	"context"
	"fmt"

	"github.com/mi2428/dropcheck-agent/controller/internal/oui"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

func renderOUI(ctx context.Context, update bool) (string, error) {
	store, err := oui.Default()
	if err != nil {
		return "", fmt.Errorf("OUI database: %w", err)
	}
	var info oui.Info
	if update {
		info, err = store.Update(ctx)
	} else {
		info, err = store.Info()
	}
	if err != nil {
		return "", fmt.Errorf("OUI database: %w", err)
	}
	return render.Fields([][2]string{{"Owner", "Controller (local host)"}, {"Database", "IEEE MA-L/MA-M/MA-S/IAB"},
		{"Fetched at", info.FetchedAt}, {"Assignments", fmt.Sprint(info.Assignments)}, {"Source", info.Source},
		{"Lookup", "offline IEEE assignee; not a device model or proof of manufacturer"}}, render.Presentation{}), nil
}
