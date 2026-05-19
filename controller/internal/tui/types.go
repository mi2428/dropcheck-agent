package tui

import (
	"time"

	"dropcheck/controller/internal/watch"
	"dropcheck/controller/internal/watchstate"

	"charm.land/bubbles/v2/key"
)

type targetState = watchstate.TargetState
type stepState = watchstate.StepState
type failedCheckState = watchstate.FailedCheck
type failedCheckSummary = watchstate.FailedCheckSummary
type passingCheckState = watchstate.PassingCheck
type passingCheckSummary = watchstate.PassingCheckSummary
type failureHotspotSummary = watchstate.FailureHotspotSummary
type failureCauseSummary = watchstate.FailureCauseSummary
type eventLogEntry = watchstate.EventLogEntry
type connectState = watchstate.ConnectState
type occurrenceHistogram = watchstate.OccurrenceHistogram

type outcomeEvent = watchstate.OutcomeEvent
type checkStatusAggregate = watchstate.CheckStatusAggregate

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

type eventMsg watch.Event
type closedMsg struct{}
type tickMsg time.Time

type keyMap struct {
	quit key.Binding
}

type model struct {
	Title  string
	events <-chan watch.Event
	keys   keyMap
	width  int
	height int
	watchstate.State
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
	pauseControl              *watch.PauseController
	skipControl               *watch.SkipController
	detailOpen                bool
	detailPanel               focusPanel
	detailHotspotMode         failureHotspotMode
	detailPassingKey          string
	detailFailedKey           string
	detailHotspotKey          string
}
