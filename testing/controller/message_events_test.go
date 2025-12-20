package controller

import (
	"math/rand"
	"testing"
	"time"

	pb "github.com/distcodep7/dsnet/proto"
	"google.golang.org/protobuf/proto"
)

// fakeSender implements sender and captures sent messages
type fakeSender struct {
	sendCh chan *pb.Envelope
}

func (f *fakeSender) SendEnvelope(msg *pb.Envelope) error {
	f.sendCh <- msg
	return nil
}

func newTestRNG() *rand.Rand {
	return rand.New(rand.NewSource(1))
}

// Test probCheck behavior for edge cases and approximate probability.
// Uses a deterministic RNG so tests are stable.
func TestProbCheck(t *testing.T) {
	// rand.NewSource(1) produces a deterministic sequence.
	// First Float64() ≈ 0.604660…
	s := &Server{rng: newTestRNG()}

	// p = 0 → always false
	if s.probCheck(0) {
		t.Fatalf("expected probCheck(0) = false")
	}

	// Reset RNG so next call uses the same first Float64
	s.rng = newTestRNG()
	if !s.probCheck(1) { // 1.0 is max valid probability
		t.Fatalf("expected probCheck(1) = true")
	}

	// Reset and test a middle value
	s.rng = newTestRNG()
	r := s.rng.Float64() // save expected value
	s.rng = newTestRNG() // restore state

	p := 0.7
	got := s.probCheck(p)
	expected := r < p
	if got != expected {
		t.Fatalf("probCheck(%v) = %v, expected %v (r=%v)", p, got, expected, r)
	}
}

func TestRandIntn(t *testing.T) {
	tests := []struct {
		name        string
		n           int
		want        int
		setup       func(*Server)
		expectPanic bool
	}{
		{"first call", 10, 1, nil, false},
		{"second call", 10, 7, nil, false},
		{"panic without RNG", 5, 0, func(s *Server) { s.rng = nil }, true},
	}

	// Pre-initialize RNG for first two tests
	s := &Server{}
	s.rng = newTestRNG()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("TEST: %s", tc.name)

			if tc.setup != nil {
				tc.setup(s)
			}
			defer func() {
				r := recover()
				if tc.expectPanic && r == nil {
					t.Fatalf("expected panic, got none")
				}
				if !tc.expectPanic && r != nil {
					t.Fatalf("unexpected panic: %v", r)
				}
			}()

			got := s.randIntn(tc.n)
			if !tc.expectPanic && got != tc.want {
				t.Fatalf("randIntn(%d) = %d, want %d", tc.n, got, tc.want)
			}
		})
	}
}

func TestDropMessage(t *testing.T) {
	s := &Server{
		nodes:      make(map[string]*Node),
		blocked:    make(map[string]map[string]bool),
		testConfig: TestConfig{},
	}

	t.Run("nil_message_returns_error", func(t *testing.T) {
		err := s.dropMessage(nil)
		if err == nil {
			t.Fatal("expected error when dropping nil message, got nil")
		}
	})

	t.Run("valid_message_succeeds", func(t *testing.T) {
		msg := &pb.Envelope{
			From:    "A",
			To:      "B",
			Type:    "TEST",
			Payload: `{"hello":"world"}`,
		}

		err := s.dropMessage(msg)
		if err != nil {
			t.Fatalf("unexpected error when dropping valid message: %v", err)
		}
	})
}

func TestDuplicateMessage(t *testing.T) {
	msg := &pb.Envelope{From: "A", To: "B", Type: "TEST", Payload: "{}"}

	cases := []struct {
		name      string
		async     bool
		target    bool // whether the recipient exists
		expectMsg bool
	}{
		{"sync_with_target", false, true, true},
		{"async_with_target", true, true, true},
		{"sync_no_target", false, false, false},
		{"async_no_target", true, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("TEST: %s", tc.name)
			sendCh := make(chan *pb.Envelope, 1)
			senders := make(map[string]sender)

			if tc.target {
				fs := &fakeSender{sendCh: sendCh}
				senders["B"] = fs
			}

			s := &Server{
				senders:    senders,
				testConfig: TestConfig{AsyncDuplicate: tc.async},
			}

			err := s.duplicateMessage(msg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.expectMsg {
				select {
				case sent := <-sendCh:
					if sent == msg {
						t.Errorf("message was not cloned")
					}
					if !proto.Equal(sent, msg) {
						t.Errorf("sent message differs from original: %+v", sent)
					}
				case <-time.After(time.Second):
					t.Fatal("expected message to be sent, but nothing was received")
				}
			} else {
				select {
				case sent := <-sendCh:
					t.Fatalf("did not expect send, but got %+v", sent)
				default:
					// ok
				}
			}

			// Allow async goroutine to complete
			if tc.async {
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

// TestReorderMessage ensures messages are scheduled with a delay and eventually sent.
func TestReorderMessage(t *testing.T) {
	msg := &pb.Envelope{From: "A", To: "B", Type: "TEST", Payload: "{}"}

	sendCh := make(chan *pb.Envelope, 1)
	fs := &fakeSender{sendCh: sendCh}

	s := &Server{
		senders: map[string]sender{"B": fs},
		testConfig: TestConfig{
			DisableMessageDelays: false,
			MsgDelayMin:          100,
			MsgDelayMax:          100,
			EnableNetworkSpikes:  false,
		},
	}

	start := time.Now()
	s.reorderMessage(msg)

	select {
	case sent := <-sendCh:
		if !proto.Equal(sent, msg) {
			t.Fatalf("sent message differs from original: %+v", sent)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected message to be sent after delay, but nothing was received")
	}

	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Errorf("message sent too early: elapsed=%v", elapsed)
	}
}

// TestReorderMessage_DelayedInterleave tests that a message can be received during another message's delay period.
func TestReorderMessage_DelayedInterleave(t *testing.T) {
	tests := []struct {
		name                string
		reorderMilliseconds int           // must be int because TestConfig uses int
		reorderDuration     time.Duration // actual delay used for measuring
		interleaveMsg       *pb.Envelope
		delayedMsg          *pb.Envelope
		expectOrder         []string
	}{
		{
			name:                "delayed_then_interleave",
			reorderMilliseconds: 100,
			reorderDuration:     100 * time.Millisecond,
			delayedMsg:          &pb.Envelope{From: "A", To: "B", Type: "DELAYED", Payload: "{}"},
			interleaveMsg:       &pb.Envelope{From: "A", To: "B", Type: "IMMEDIATE", Payload: "{}"},
			expectOrder:         []string{"IMMEDIATE", "DELAYED"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			sendCh := make(chan *pb.Envelope, 10)
			fs := &fakeSender{sendCh: sendCh}

			s := &Server{
				senders: map[string]sender{"B": fs},
				testConfig: TestConfig{
					MsgDelayMin: tc.reorderMilliseconds,
					MsgDelayMax: tc.reorderMilliseconds,
				},
				rng: newTestRNG(),
			}

			start := time.Now()

			// schedule delayed message
			skipped, err := s.reorderMessage(tc.delayedMsg)
			if err != nil {
				t.Fatalf("ReorderMessage returned error: %v", err)
			}
			if !skipped {
				t.Fatalf("ReorderMessage should skip immediate delivery")
			}

			// send immediate message
			if err := fs.SendEnvelope(tc.interleaveMsg); err != nil {
				t.Fatalf("interleave send failed: %v", err)
			}

			// First expected message
			first := <-sendCh
			if first.Type != tc.expectOrder[0] {
				t.Fatalf("expected first=%s, got=%s", tc.expectOrder[0], first.Type)
			}

			// Second expected message (delayed)
			select {
			case second := <-sendCh:
				if second.Type != tc.expectOrder[1] {
					t.Fatalf("expected second=%s, got=%s", tc.expectOrder[1], second.Type)
				}

				elapsed := time.Since(start)
				if elapsed < tc.reorderDuration {
					t.Fatalf("delayed message arrived too early: %v < %v", elapsed, tc.reorderDuration)
				}

			case <-time.After(tc.reorderDuration + 500*time.Millisecond):
				t.Fatalf("never received delayed message")
			}
		})
	}
}

// TestHandleMessageEvents tests the combined behavior of drop, duplicate, and reorder.
func TestHandleMessageEvents(t *testing.T) {
	msg := &pb.Envelope{From: "A", To: "B", Type: "TEST", Payload: "{}"}

	tests := []struct {
		name                   string
		dropProb               float64
		dupeProb               float64
		disableReorderMessages bool
		expectSkip             bool
		expectDuplicate        bool
	}{
		{"drop only", 1, 0, true, true, false},
		{"duplicate only", 0, 1, true, false, true},
		{"reorder only", 0, 0, false, true, false},
		{"none", 0, 0, true, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sendCh := make(chan *pb.Envelope, 1)

			// Setup server with deterministic probabilities
			s := &Server{
				nodes: map[string]*Node{
					"B": {
						id: "B",
						// Use fakeSender for testable message sending
						stream: nil,
					},
				},
				senders: map[string]sender{
					"B": &fakeSender{sendCh: sendCh},
				},
				testConfig: TestConfig{
					MsgDropProb:          tc.dropProb,
					MsgDupeProb:          tc.dupeProb,
					DisableMessageDelays: tc.disableReorderMessages,
				},
				rng: newTestRNG(), // deterministic
			}
			s.nodes["B"].alive.Store(true)

			skip, err := s.handleMessageEvents(msg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if skip != tc.expectSkip {
				t.Errorf("expected skip=%v, got %v", tc.expectSkip, skip)
			}

			if tc.expectDuplicate {
				select {
				case dup := <-sendCh:
					if dup.From != msg.From || dup.To != msg.To || dup.Type != msg.Type || dup.Payload != msg.Payload {
						t.Errorf("duplicated message differs: %+v", dup)
					}
				case <-time.After(time.Second):
					t.Fatal("expected duplicated message but none sent")
				}
			} else {
				select {
				case dup := <-sendCh:
					t.Errorf("did not expect duplicate but got: %+v", dup)
				default:
				}
			}
		})
	}
}

// TestNetworkSpikeDelay ensures that enabling network spikes can introduce additional delay.
func TestNetworkSpikes(t *testing.T) {
	tests := []struct {
		name           string
		smallProb      float64
		medProb        float64
		largeProb      float64
		expectedMillis int // minimum expected delay in milliseconds
	}{
		{"small_spike", 1.0, 0.0, 0.0, 5},   // small spike yields 5..20ms
		{"med_spike", 0.0, 1.0, 0.0, 20},    // med spike yields 20..100ms
		{"large_spike", 0.0, 0.0, 1.0, 100}, // large spike yields 100..500ms
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &pb.Envelope{From: "A", To: "B", Type: "TEST", Payload: "{}"}

			sendCh := make(chan *pb.Envelope, 1)
			fs := &fakeSender{sendCh: sendCh}

			// ensure base delay is small so spikes produce larger values
			s := &Server{
				senders: map[string]sender{"B": fs},
				testConfig: TestConfig{
					DisableMessageDelays: false,
					MsgDelayMin:          1,
					MsgDelayMax:          1,
					EnableNetworkSpikes:  true,
					NetSpikeSmallProb:    tc.smallProb,
					NetSpikeMedProb:      tc.medProb,
					NetSpikeLargeProb:    tc.largeProb,
				},
				rng: newTestRNG(),
			}

			start := time.Now()
			skipped, err := s.reorderMessage(msg)
			if err != nil {
				t.Fatalf("reorderMessage returned error: %v", err)
			}
			if !skipped {
				t.Fatalf("reorderMessage should skip immediate delivery when delayed")
			}

			select {
			case <-sendCh:
				elapsed := time.Since(start)
				if elapsed < time.Duration(tc.expectedMillis)*time.Millisecond {
					t.Fatalf("message sent too early with spike: elapsed=%v, want>= %dms", elapsed, tc.expectedMillis)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("expected message to be sent after spike delay, but nothing was received")
			}
		})
	}
}
