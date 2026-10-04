package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"dropcheck/controller/internal/control"
)

const (
	ScopeRun    = "run"
	ScopeAgent  = "agent"
	ScopeTarget = "target"
	ScopeCheck  = "check"
)

var errOperatorSkip = errors.New("skipped by operator")
var errOperatorCancel = errors.New("canceled by operator")

// Controls owns requests for one Execute invocation. Scope is explicit; cursor
// focus and empty selectors never imply global execution control.
type Controls struct {
	mu                        sync.Mutex
	valid                     map[string]Scope
	paused, skipped, canceled map[string]bool
	active                    map[uint64]controlledContext
	next                      uint64
	wake                      chan struct{}
	bound                     bool
	stop                      context.CancelCauseFunc
	emit                      func(Event) error
}

type controlledContext struct {
	scope  Scope
	cancel context.CancelCauseFunc
}

func NewControls() *Controls {
	return &Controls{valid: map[string]Scope{}, paused: map[string]bool{}, skipped: map[string]bool{}, canceled: map[string]bool{}, active: map[uint64]controlledContext{}, wake: make(chan struct{})}
}

func (s Scope) key() string {
	return strings.Join([]string{s.Kind, s.AgentID, s.TargetID, s.CheckID}, "\x00")
}
func (s Scope) contains(other Scope) bool {
	switch s.Kind {
	case ScopeRun:
		return true
	case ScopeAgent:
		return s.AgentID == other.AgentID
	case ScopeTarget:
		return s.AgentID == other.AgentID && s.TargetID == other.TargetID
	case ScopeCheck:
		return s.AgentID == other.AgentID && s.TargetID == other.TargetID && s.CheckID == other.CheckID
	}
	return false
}

func (c *Controls) bind(p *CompiledPlan, stop context.CancelCauseFunc, emit func(Event) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bound || c.stop != nil {
		return fmt.Errorf("controls are single-run; create new controls")
	}
	c.bound, c.stop, c.emit = true, stop, emit
	c.valid[Scope{Kind: ScopeRun}.key()] = Scope{Kind: ScopeRun}
	for _, target := range p.targets {
		for _, agent := range target.agents {
			for _, scope := range []Scope{{Kind: ScopeAgent, AgentID: agentKey(agent)}, {Kind: ScopeTarget, AgentID: agentKey(agent), TargetID: target.preview.ID}} {
				c.valid[scope.key()] = scope
			}
			for _, check := range append([]compiledStep{target.connect, target.wait}, target.checks...) {
				if check.id == "" {
					continue
				}
				scope := Scope{Kind: ScopeCheck, AgentID: agentKey(agent), TargetID: target.preview.ID, CheckID: check.id}
				c.valid[scope.key()] = scope
			}
		}
	}
	return nil
}

func (c *Controls) Pause(scope Scope) error  { return c.request("pause", scope) }
func (c *Controls) Resume(scope Scope) error { return c.request("resume", scope) }
func (c *Controls) Skip(scope Scope) error   { return c.request("skip", scope) }
func (c *Controls) Cancel(scope Scope) error { return c.request("cancel", scope) }

func (c *Controls) request(action string, scope Scope) error {
	if c == nil {
		return fmt.Errorf("controls are nil")
	}
	c.mu.Lock()
	if !c.bound {
		c.mu.Unlock()
		return fmt.Errorf("run is not active")
	}
	if _, ok := c.valid[scope.key()]; !ok {
		c.mu.Unlock()
		return fmt.Errorf("unknown control scope")
	}
	switch action {
	case "pause":
		c.paused[scope.key()] = true
	case "resume":
		delete(c.paused, scope.key())
	case "skip":
		c.skipped[scope.key()] = true
	case "cancel":
		c.canceled[scope.key()] = true
	}
	if action == "skip" || action == "cancel" {
		cause := errOperatorSkip
		if action == "cancel" {
			cause = errOperatorCancel
		}
		for _, active := range c.active {
			if scope.contains(active.scope) {
				active.cancel(cause)
			}
		}
		if action == "cancel" && scope.Kind == ScopeRun {
			c.stop(cause)
		}
	}
	close(c.wake)
	c.wake = make(chan struct{})
	emit := c.emit
	c.mu.Unlock()
	return emit(Event{Kind: EventControlRequested, Scope: scope, Status: action, Message: action + " requested"})
}

func (c *Controls) Paused(scope Scope) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.paused {
		if c.valid[key].contains(scope) {
			return true
		}
	}
	return false
}

func (c *Controls) context(parent context.Context, scope Scope) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	c.mu.Lock()
	id := c.next
	c.next++
	c.active[id] = controlledContext{scope: scope, cancel: cancel}
	for key := range c.canceled {
		if c.valid[key].contains(scope) {
			cancel(errOperatorCancel)
		}
	}
	for key := range c.skipped {
		if c.valid[key].contains(scope) {
			cancel(errOperatorSkip)
		}
	}
	c.mu.Unlock()
	return ctx, func() { c.mu.Lock(); delete(c.active, id); c.mu.Unlock(); cancel(nil) }
}

func (c *Controls) wait(ctx context.Context, scope Scope) error {
	ack := false
	for {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		c.mu.Lock()
		paused := false
		for key := range c.paused {
			if c.valid[key].contains(scope) {
				paused = true
				break
			}
		}
		wake := c.wake
		c.mu.Unlock()
		if !paused {
			if ack {
				return c.emit(Event{Kind: EventControlApplied, Scope: scope, Status: "running", Message: "resumed"})
			}
			return nil
		}
		if !ack {
			if err := c.emit(Event{Kind: EventControlApplied, Scope: scope, Status: "paused", Message: "paused at operation boundary"}); err != nil {
				return err
			}
			ack = true
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-wake:
		}
	}
}

func (c *Controls) nextRound() { c.mu.Lock(); clear(c.skipped); c.mu.Unlock() }
func (c *Controls) finish()    { c.mu.Lock(); c.bound = false; c.mu.Unlock() }

func agentKey(agent control.AgentInfo) string {
	if agent.ID != "" {
		return agent.ID
	}
	return "agent/0" // Injected single-agent runners need no transport identifier.
}
