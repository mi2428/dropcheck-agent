package tui

import (
	"charm.land/lipgloss/v2"
)

var (
	appStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("#C87528")).Background(lipgloss.Color("#1A0A02"))
	borderStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("#A95A18")).Background(lipgloss.Color("#1A0A02"))
	focusBorderStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#F0903A")).Background(lipgloss.Color("#1A0A02"))
	titleStyle              = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C87528")).Background(lipgloss.Color("#1A0A02"))
	focusTitleStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#1A0A02"))
	keyStyle                = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58F2A5")).Background(lipgloss.Color("#1A0A02"))
	valueStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE8822")).Background(lipgloss.Color("#1A0A02"))
	logStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("#B46522")).Background(lipgloss.Color("#1A0A02"))
	groupStyle              = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58F2A5")).Background(lipgloss.Color("#1A0A02"))
	tableHeaderStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58F2A5")).Background(lipgloss.Color("#3A1203"))
	summaryKeyStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#1A0A02"))
	summaryValueStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE8822")).Background(lipgloss.Color("#1A0A02"))
	summaryFreshRowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#1A0A02"))
	summaryStaleRowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#B46522")).Background(lipgloss.Color("#1A0A02"))
	summaryTableHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#3A1203"))
	summaryGraphStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9F1C")).Background(lipgloss.Color("#1A0A02"))
	selectedStyle           = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#5C1802"))
	okStatusStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("#86C779")).Background(lipgloss.Color("#1A0A02"))
	failedStatusStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6B4A")).Background(lipgloss.Color("#1A0A02"))
	runningStatusStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD166")).Background(lipgloss.Color("#1A0A02"))
	waitStatusStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#9B6A43")).Background(lipgloss.Color("#1A0A02"))
	skippedStatusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#D28A3C")).Background(lipgloss.Color("#1A0A02"))
	staleOKStatusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#3C9D62")).Background(lipgloss.Color("#1A0A02"))
	staleFailedStatusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#B45A38")).Background(lipgloss.Color("#1A0A02"))
	staleSkippedStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#9C6436")).Background(lipgloss.Color("#1A0A02"))
	warnStyle               = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD166")).Background(lipgloss.Color("#1A0A02"))
	okGraphStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("#B7D866")).Background(lipgloss.Color("#1A0A02"))
	timelineBaseStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#4F9F67")).Background(lipgloss.Color("#1A0A02"))
	failGraphStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6B4A")).Background(lipgloss.Color("#1A0A02"))
	connectFailureStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF2D55")).Background(lipgloss.Color("#1A0A02"))
	dimStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B4A37")).Background(lipgloss.Color("#1A0A02"))
	panelRowAnchorStyles    = []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1A0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1B0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1C0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1D0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1E0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1F0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#200A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#210A02")).Background(lipgloss.Color("#1A0A02")),
	}
)
