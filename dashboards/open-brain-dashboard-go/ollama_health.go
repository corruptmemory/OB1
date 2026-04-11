package main

import (
	"context"
	"time"
)

// healthState holds the last known up/down timestamps. Zero values
// mean "never observed." Only the actor goroutine reads or writes
// this struct directly.
type healthState struct {
	lastUp   time.Time
	lastDown time.Time
}

type healthCmd struct {
	kind string // "up" | "down" | "get"
	resp chan healthState
}

// OllamaHealth is an actor-pattern wrapper around the last observed
// state of the Ollama embedding endpoint. A single goroutine owns
// the state; handlers send typed commands over the channel to record
// successes, failures, or to read the current state for rendering.
//
// The actor pattern here is overkill for just two timestamps, but
// it's the convention Jim uses for any shared mutable state in his
// Go web apps — see the actor-pattern skill and CLAUDE.md.
type OllamaHealth struct {
	cmds chan healthCmd
}

// NewOllamaHealth starts the actor goroutine and returns a handle.
// The goroutine exits when ctx is cancelled (i.e., when the server
// shuts down).
func NewOllamaHealth(ctx context.Context) *OllamaHealth {
	h := &OllamaHealth{cmds: make(chan healthCmd, 32)}
	go h.run(ctx)
	return h
}

func (h *OllamaHealth) run(ctx context.Context) {
	var state healthState
	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-h.cmds:
			switch cmd.kind {
			case "up":
				state.lastUp = time.Now()
			case "down":
				state.lastDown = time.Now()
			case "get":
				cmd.resp <- state
			}
		}
	}
}

// RecordUp is a fire-and-forget message to mark the endpoint healthy.
func (h *OllamaHealth) RecordUp() {
	h.cmds <- healthCmd{kind: "up"}
}

// RecordDown is a fire-and-forget message to mark the endpoint
// unhealthy (embed call failed).
func (h *OllamaHealth) RecordDown() {
	h.cmds <- healthCmd{kind: "down"}
}

// Status returns the displayable tri-state of the indicator:
//   - "green":  last observed state was up within 5 minutes
//   - "amber":  last observed state was down, or up but stale
//   - "grey":   never observed (fresh server, no embed calls yet)
func (h *OllamaHealth) Status() string {
	resp := make(chan healthState, 1)
	h.cmds <- healthCmd{kind: "get", resp: resp}
	s := <-resp

	if s.lastUp.IsZero() && s.lastDown.IsZero() {
		return "grey"
	}
	// If the last observation was a failure, we're amber regardless
	// of how old any previous success is.
	if s.lastDown.After(s.lastUp) {
		return "amber"
	}
	if time.Since(s.lastUp) > 5*time.Minute {
		return "amber"
	}
	return "green"
}
