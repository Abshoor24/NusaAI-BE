package handler

import (
	"context"
	"sync"
	"time"
)

// phaseContext is a context whose deadline can be changed after creation.
// It exists because one provider HTTP request must first be bounded by the
// short open-phase budget and, once headers arrive, by the longer stream
// deadline instead. A normal context.WithTimeout would cancel the stream at
// the open deadline.
type phaseContext struct {
	parent context.Context
	done   chan struct{}

	mu       sync.Mutex
	err      error
	deadline time.Time
	timer    *time.Timer
	closed   bool
}

func newPhaseContext(parent context.Context) *phaseContext {
	c := &phaseContext{parent: parent, done: make(chan struct{})}
	go func() {
		select {
		case <-parent.Done():
			c.cancel(parent.Err())
		case <-c.done:
		}
	}()
	return c
}

func (c *phaseContext) Deadline() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deadline.IsZero() {
		return time.Time{}, false
	}
	return c.deadline, true
}

func (c *phaseContext) Done() <-chan struct{} { return c.done }

func (c *phaseContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *phaseContext) Value(key any) any { return c.parent.Value(key) }

// SetDeadline replaces the current deadline (and its timer).
func (c *phaseContext) SetDeadline(deadline time.Time) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.deadline = deadline
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(time.Until(deadline), func() {
		c.cancel(context.DeadlineExceeded)
	})
	c.mu.Unlock()
}

// cancel records err and closes Done exactly once.
func (c *phaseContext) cancel(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if err == nil {
		err = context.Canceled
	}
	c.err = err
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	close(c.done)
	c.mu.Unlock()
}

// Cancel ends the context with the given error (context.Canceled by default).
func (c *phaseContext) Cancel(err error) { c.cancel(err) }
