package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/distcodep7/dsnet/proto"
	ctrl "github.com/distcodep7/dsnet/testing/controller" // adjust path if different
	"github.com/distcodep7/dsnet/testing/wrapper"
)

// Observation is a normalized, easy-to-query view of a controller event.
type Observation struct {
	Kind     string    // copy of ControllerEvent.Kind
	From     string
	To       string
	Type     string    // Envelope.Type
	Payload  []byte    // Envelope.Payload as raw bytes
	Lamport  uint64
	Time     time.Time
	RawEvent *ctrl.ControllerEvent
}

// Harness is the test harness for a job run. It subscribes to controller events,
// retains a trace, and provides helpers to inject triggers and query the trace.
type Harness struct {
	Ctrl       *ctrl.Server
	WM         *wrapper.WrapperManager // optional (for wrapper start/stop)
	events     chan *ctrl.ControllerEvent
	traceMu    sync.RWMutex
	trace      []*Observation
	closed     atomicBool
}

// atomicBool lightweight helper
type atomicBool struct {
	v int32
}
func (b *atomicBool) Load() bool { return atomic.LoadInt32(&b.v) != 0 }
func (b *atomicBool) Store(v bool) { if v { atomic.StoreInt32(&b.v,1) } else { atomic.StoreInt32(&b.v,0) } }

const defaultEventBuf = 4096

// NewHarness registers an observer channel on the controller and returns a Harness.
// eventsBuf controls how many controller events are buffered.
func NewHarness(ctrlSrv *ctrl.Server, wm *wrapper.WrapperManager, eventsBuf int) *Harness {
	if eventsBuf <= 0 {
		eventsBuf = defaultEventBuf
	}
	events := make(chan *ctrl.ControllerEvent, eventsBuf)
	h := &Harness{
		Ctrl:   ctrlSrv,
		WM:     wm,
		events: events,
		trace:  make([]*Observation, 0, 1024),
	}
	ctrlSrv.RegisterObserver(events)
	go h.loop()
	return h
}

// Close unregisters and stops the harness observer.
func (h *Harness) Close() {
	if h.closed.Load() {
		return
	}
	h.closed.Store(true)
	h.Ctrl.UnregisterObserver(h.events)
	close(h.events)
	h.traceMu.Lock()
	h.trace = nil
	h.traceMu.Unlock()
}

// loop consumes controller events and appends normalized observations to in-memory trace.
func (h *Harness) loop() {
	for ev := range h.events {
		if ev == nil {
			continue
		}
		obs := &Observation{
			Kind:     ev.Kind,
			Time:     ev.RecvTime,
			RawEvent: ev,
		}
		if ev.Env != nil {
			obs.From = ev.Env.From
			obs.To = ev.Env.To
			obs.Type = ev.Env.Type
			obs.Payload = []byte(ev.Env.Payload)
			obs.Lamport = ev.Env.Lamport
		}
		h.traceMu.Lock()
		h.trace = append(h.trace, obs)
		h.traceMu.Unlock()
	}
}

// SnapshotTrace returns a copy of the current trace (safe for analysis).
func (h *Harness) SnapshotTrace() []*Observation {
	h.traceMu.RLock()
	defer h.traceMu.RUnlock()
	out := make([]*Observation, len(h.trace))
	copy(out, h.trace)
	return out
}

// Inject builds an envelope and injects it to the controller (convenience).
// The payload parameter will be JSON marshaled.
func (h *Harness) Inject(ctx context.Context, from, to, typ string, payload interface{}, lamport uint64) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	env := &pb.Envelope{
		From:    from,
		To:      to,
		Type:    typ,
		Payload: string(b),
		Lamport: lamport,
	}
	return h.Ctrl.InjectEnvelope(env)
}

// WaitFor predicate waits for up to timeout for an observation matching pred, returns observation or nil.
func (h *Harness) WaitFor(ctx context.Context, timeout time.Duration, pred func(*Observation) bool) *Observation {
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx2.Done():
			// last-chance scan
			trace := h.SnapshotTrace()
			for _, o := range trace {
				if pred(o) {
					return o
				}
			}
			return nil
		case <-ticker.C:
			trace := h.SnapshotTrace()
			for _, o := range trace {
				if pred(o) {
					return o
				}
			}
		}
	}
}
