package tui

import (
	"charm.land/lipgloss/v2"
)

var (
	colorBackground       = lipgloss.Color("#1A0A02")
	colorHeaderBackground = lipgloss.Color("#3A1203")
	colorSelectedBg       = lipgloss.Color("#5C1802")
	colorAppText          = lipgloss.Color("#C87528")
	colorBorder           = lipgloss.Color("#A95A18")
	colorFocusBorder      = lipgloss.Color("#F0903A")
	colorOrangeStrong     = lipgloss.Color("#FFAA44")
	colorOrange           = lipgloss.Color("#EE8822")
	colorOrangeMuted      = lipgloss.Color("#B46522")
	colorOrangeDim        = lipgloss.Color("#9B6A43")
	colorSkipped          = lipgloss.Color("#D28A3C")
	colorStaleSkipped     = lipgloss.Color("#9C6436")
	colorBrilliantGreen   = lipgloss.Color("#58F2A5")
	colorSuccess          = lipgloss.Color("#86C779")
	colorSuccessDim       = lipgloss.Color("#3C9D62")
	colorFailure          = lipgloss.Color("#FF6B4A")
	colorFailureDim       = lipgloss.Color("#B45A38")
	colorDim              = lipgloss.Color("#6B4A37")

	appStyle                   = lipgloss.NewStyle().Foreground(colorAppText).Background(colorBackground)
	borderStyle                = lipgloss.NewStyle().Foreground(colorBorder).Background(colorBackground)
	focusBorderStyle           = lipgloss.NewStyle().Foreground(colorFocusBorder).Background(colorBackground)
	titleStyle                 = lipgloss.NewStyle().Bold(true).Foreground(colorAppText).Background(colorBackground)
	focusTitleStyle            = lipgloss.NewStyle().Bold(true).Foreground(colorOrangeStrong).Background(colorBackground)
	keyStyle                   = lipgloss.NewStyle().Bold(true).Foreground(colorBrilliantGreen).Background(colorBackground)
	valueStyle                 = lipgloss.NewStyle().Foreground(colorOrange).Background(colorBackground)
	logStyle                   = lipgloss.NewStyle().Foreground(colorOrangeMuted).Background(colorBackground)
	groupStyle                 = lipgloss.NewStyle().Bold(true).Foreground(colorBrilliantGreen).Background(colorBackground)
	tableHeaderStyle           = lipgloss.NewStyle().Bold(true).Foreground(colorBrilliantGreen).Background(colorHeaderBackground)
	summaryKeyStyle            = lipgloss.NewStyle().Bold(true).Foreground(colorBrilliantGreen).Background(colorBackground)
	summaryValueStyle          = lipgloss.NewStyle().Foreground(colorOrange).Background(colorBackground)
	summaryFreshRowStyle       = lipgloss.NewStyle().Foreground(colorOrangeStrong).Background(colorBackground)
	summaryWarmRowStyle        = lipgloss.NewStyle().Foreground(colorOrange).Background(colorBackground)
	summaryFailureRowStyle     = lipgloss.NewStyle().Foreground(colorFailure).Background(colorBackground)
	summaryStaleRowStyle       = lipgloss.NewStyle().Foreground(colorOrangeMuted).Background(colorBackground)
	summaryTableHeaderStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorOrangeStrong).Background(colorHeaderBackground)
	summarySparklineLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBrilliantGreen).Background(colorBackground)
	summaryGraphLabelStyle     = lipgloss.NewStyle().Bold(true).Foreground(colorOrangeStrong).Background(colorBackground)
	summaryGraphStyle          = lipgloss.NewStyle().Foreground(colorOrangeStrong).Background(colorBackground)
	timelineKeyStyle           = lipgloss.NewStyle().Bold(true).Foreground(colorOrangeStrong).Background(colorBackground)
	timelineValueStyle         = lipgloss.NewStyle().Foreground(colorOrange).Background(colorBackground)
	selectedStyle              = lipgloss.NewStyle().Bold(true).Foreground(colorOrangeStrong).Background(colorSelectedBg)
	okStatusStyle              = lipgloss.NewStyle().Foreground(colorSuccess).Background(colorBackground)
	failedStatusStyle          = lipgloss.NewStyle().Bold(true).Foreground(colorFailure).Background(colorBackground)
	runningStatusStyle         = lipgloss.NewStyle().Bold(true).Foreground(colorOrangeStrong).Background(colorBackground)
	waitStatusStyle            = lipgloss.NewStyle().Foreground(colorOrangeDim).Background(colorBackground)
	skippedStatusStyle         = lipgloss.NewStyle().Foreground(colorSkipped).Background(colorBackground)
	staleOKStatusStyle         = lipgloss.NewStyle().Foreground(colorSuccessDim).Background(colorBackground)
	staleFailedStatusStyle     = lipgloss.NewStyle().Foreground(colorFailureDim).Background(colorBackground)
	staleSkippedStatusStyle    = lipgloss.NewStyle().Foreground(colorStaleSkipped).Background(colorBackground)
	warnStyle                  = lipgloss.NewStyle().Bold(true).Foreground(colorOrangeStrong).Background(colorBackground)
	okGraphStyle               = lipgloss.NewStyle().Foreground(colorSuccess).Background(colorBackground)
	timelineBaseStyle          = lipgloss.NewStyle().Foreground(colorOrangeMuted).Background(colorBackground)
	failGraphStyle             = lipgloss.NewStyle().Bold(true).Foreground(colorFailure).Background(colorBackground)
	connectFailureStyle        = lipgloss.NewStyle().Bold(true).Foreground(colorFailure).Background(colorBackground)
	dimStyle                   = lipgloss.NewStyle().Foreground(colorDim).Background(colorBackground)
	panelRowAnchorStyles       = []lipgloss.Style{
		lipgloss.NewStyle().Foreground(colorBackground).Background(colorBackground),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1B0A02")).Background(colorBackground),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1C0A02")).Background(colorBackground),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1D0A02")).Background(colorBackground),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1E0A02")).Background(colorBackground),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1F0A02")).Background(colorBackground),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#200A02")).Background(colorBackground),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#210A02")).Background(colorBackground),
	}
)
