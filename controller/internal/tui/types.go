package tui

import (
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuistate"

	"charm.land/bubbles/v2/key"
)

type targetState = tuistate.TargetState
type stepState = tuistate.StepState
type failedCheckState = tuistate.FailedCheck
type failedCheckSummary = tuistate.FailedCheckSummary
type passingCheckState = tuistate.PassingCheck
type passingCheckSummary = tuistate.PassingCheckSummary
type failureHotspotSummary = tuistate.FailureHotspotSummary
type failureCauseSummary = tuistate.FailureCauseSummary
type eventLogEntry = tuistate.EventLogEntry
type connectState = tuistate.ConnectState
type occurrenceHistogram = tuistate.OccurrenceHistogram

type outcomeEvent = tuistate.OutcomeEvent
type checkStatusAggregate = tuistate.CheckStatusAggregate

type targetRoundBucket struct {
	Seen          bool
	Failed        int
	ConnectFailed bool
}

type runQueueLine struct {
	Text    string
	Status  string
	Current bool
}

type focusPanel int

const (
	focusPassingChecks focusPanel = iota
	focusFailedChecks
	focusCheckStatus
	focusRunQueue
	focusFailureHotspots
)

type focusSlot struct {
	Panel            focusPanel
	HotspotAgentKey  string
	RunQueueAgentKey string
}

type failureHotspotMode int

const (
	failureHotspotModeTargets failureHotspotMode = iota
	failureHotspotModeCauses
)

const (
	roundTimelineMinVisibleRounds = 10
	roundTimelineTargetLabelRunes = 10
	roundTimelineTileGap          = 1
	summarySparklineRows          = 5
	summarySparklineWindow        = 30 * time.Minute
	detailTimelineWindow          = 90 * time.Minute
	investigationHistoryWindow    = 24 * time.Hour
	checkHistoryRetentionWindow   = investigationHistoryWindow
	eventLogRetentionWindow       = investigationHistoryWindow
	maxPassingCheckHistory        = 200000
	maxFailedCheckHistory         = 100000
	maxEventLogHistory            = 100000
	visibleEventLogLimit          = 400
	detailModalWidthPercent       = 54
	detailModalHeightPercent      = 33
	detailModalMaxHeightPercent   = 80
	detailModalLogLimit           = 120
	recencyFreshWindow            = 15 * time.Second
	recencyWarmWindow             = 30 * time.Second
)

type eventMsg tuirun.Event
type closedMsg struct{}
type tickMsg time.Time

type keyMap struct {
	quit key.Binding
}

type model struct {
	Title  string
	events <-chan tuirun.Event
	keys   keyMap
	width  int
	height int
	tuistate.State
	closed                    bool
	focus                     focusPanel
	focusHotspotAgentKey      string
	focusRunQueueAgentKey     string
	passingCheckCursor        int
	passingCheckPinned        bool
	passingCheckPinnedKey     string
	failedCheckCursor         int
	failedCheckPinned         bool
	failedCheckPinnedKey      string
	failureHotspotCursor      int
	failureHotspotMode        failureHotspotMode
	failureHotspotPinned      bool
	failureHotspotKey         string
	checkStatusOffset         int
	checkStatusPinned         bool
	runQueueCursor            int
	runQueueOffset            int
	runQueuePinned            bool
	searchEditing             bool
	searchPanel               focusPanel
	passingSearchQuery        string
	failedSearchQuery         string
	checkStatusSearchQuery    string
	failureHotspotSearchQuery string
	paused                    bool
	controls                  *tuirun.Controls
	detailOpen                bool
	detailPanel               focusPanel
	detailHotspotMode         failureHotspotMode
	detailPassingKey          string
	detailFailedKey           string
	detailHotspotKey          string
}
