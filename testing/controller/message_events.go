package controller

import (
	"fmt"
	"log"
	"time"

	pb "github.com/distcodep7/dsnet/proto"
	"google.golang.org/protobuf/proto"
)

func isTesterMsg(msg *pb.Envelope) bool {
	return msg.From == "TESTER" || msg.To == "TESTER"
}

func isControlMsg(msg *pb.Envelope) bool {
	return msg.From == "CTRL" || msg.To == "CTRL"
}

// probCheck returns true with probability p.
func (s *Server) probCheck(p float64) bool {
	s.rngMu.Lock()
	r := s.rng.Float64()
	s.rngMu.Unlock()
	return r < p
}

// randIntn returns a non-negative pseudo-random int in [0,n).
func (s *Server) randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	s.rngMu.Lock()
	v := s.rng.Intn(n)
	s.rngMu.Unlock()
	return v
}

// handleMessageEvents processes message events (drop, duplicate, reorder).
// It returns (true, nil) if the message delivery should be skipped (dropped or scheduled for later).
// It returns (false, nil) if the message should be sent immediately.
// It returns (false, err) if an error occurred during processing.
func (s *Server) handleMessageEvents(msg *pb.Envelope) (bool, error) {
	if isTesterMsg(msg) || isControlMsg(msg) {
		return false, nil
	}

	if s.probCheck(s.testConfig.DropProb) {
		if err := s.dropMessage(msg); err != nil {
			return false, fmt.Errorf("[DROP ERR] %v", err)
		}
		return true, nil
	}

	if s.probCheck(s.testConfig.DupeProb) {
		if err := s.duplicateMessage(msg); err != nil {
			return false, fmt.Errorf("[DUPE ERR] %v", err)
		}
		return false, nil
	}

	if s.testConfig.ReorderMessages {
		scheduled, err := s.reorderMessage(msg)
		if err != nil {
			return false, fmt.Errorf("[REORD ERR] %v", err)
		}
		if scheduled {
			// message delivery delayed; do not send now
			return true, nil
		}
	}

	return false, nil
}

// dropMessage logs and drops the given message.
func (s *Server) dropMessage(msg *pb.Envelope) error {
	if msg == nil {
		return fmt.Errorf("message not found")
	}
	s.logDrop(msg)
	log.Printf("[DROP] %s Dropped: %s -> %s", msg.Type, msg.From, msg.To)
	return nil
}

func (s *Server) duplicateMessage(msg *pb.Envelope) error {
	doAsync := true
	if !s.testConfig.AsyncDuplicate {
		doAsync = false
	}

	s.mu.Lock()
	target, ok := s.senders[msg.To]
	s.mu.Unlock()
	if !ok {
		return nil
	}

	clone := proto.Clone(msg).(*pb.Envelope)

	if doAsync {
		go func(n sender, m *pb.Envelope) {
			if err := n.SendEnvelope(m); err != nil {
				log.Printf("[DUPE ERR] %v", err)
			} else {
				log.Printf("[DUPE] %s Duplicated: %s -> %s", m.Type, m.From, m.To)
			}
		}(target, clone)
		return nil
	}

	// Synchronous duplicate.
	if err := target.SendEnvelope(clone); err != nil {
		return err
	}
	log.Printf("[DUPE] %s Duplicated: %s -> %s", clone.Type, clone.From, clone.To)
	return nil
}

// reorderMessage chooses a random delay between ReorderMinDelay and ReorderMaxDelay
// (milliseconds) and schedules the delayed send.
// It returns true if the message delivery is scheduled for later, false otherwise.
func (s *Server) reorderMessage(msg *pb.Envelope) (bool, error) {
	min := s.testConfig.ReorderMinDelay
	max := s.testConfig.ReorderMaxDelay

	if min > max {
		return false, fmt.Errorf("reorderMinDelay (%d) cannot be greater than ReorderMaxDelay (%d)", min, max)
	}

	spike := 0
	if s.testConfig.NetworkSpikeEnabled {
		spike = s.latencySpike()
	}

	var duration int
	if spike > 0 {
		duration = spike
	} else if max == min {
		duration = min
	} else {
		duration = s.randIntn(max-min+1) + min
	}

	d := time.Duration(duration) * time.Millisecond
	if scheduled := s.delaySendWithDuration(msg, d); scheduled {
		return true, nil // skip immediate send; delayed goroutine will deliver
	}
	// Destination unknown right now; fall back to immediate send by returning false
	return false, nil
}

func (s *Server) latencySpike() int {
	d := 0
	if s.probCheck(s.testConfig.NetSpikeLargeProb) {
		d = 100 + s.randIntn(401)
	} else if s.probCheck(s.testConfig.NetSpikeMedProb) {
		d = 20 + s.randIntn(81)
	} else if s.probCheck(s.testConfig.NetSpikeSmallProb) {
		d = 5 + s.randIntn(16)
	}
	return d
}

// delaySendWithDuration sends a scheduled message after a set amount of time.
// delaySendWithDuration schedules a delayed delivery. Returns true if scheduled, false if destination is unknown.
func (s *Server) delaySendWithDuration(msg *pb.Envelope, d time.Duration) bool {
	s.mu.Lock()
	target, ok := s.senders[msg.To]
	s.mu.Unlock()
	if !ok {
		log.Printf("[REORD ERR] Unknown destination for delayed send: %s", msg.To)
		return false
	}

	clone := proto.Clone(msg).(*pb.Envelope)

	go func(n sender, m *pb.Envelope, d time.Duration) {
		min := time.Duration(s.testConfig.ReorderMinDelay) * time.Millisecond
		max := time.Duration(s.testConfig.ReorderMaxDelay) * time.Millisecond

		if d == 0 {
			return
		}

		isSpike := d < min || d > max
		if isSpike {
			log.Printf("[REORD] Network spike delaying %s: %s -> %s for %v", m.Type, m.From, m.To, d)
		}

		time.Sleep(d)

		if err := n.SendEnvelope(m); err != nil {
			log.Printf("[REORD ERR] failed send after delay: %v", err)
		} else if isSpike {
			log.Printf("[REORD] Sent %s delayed: %s -> %s", m.Type, m.From, m.To)
		}
	}(target, clone, d)

	return true
}
