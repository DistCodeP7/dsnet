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

//probCheck returns true with probability p.
func (s *Server) probCheck(p float64) bool {
	s.rngMu.Lock()
	r := s.rng.Float64()
	s.rngMu.Unlock()
	return r < p
}

//randIntn returns a non-negative pseudo-random int in [0,n).
func (s *Server) randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	s.rngMu.Lock()
	v := s.rng.Intn(n)
	s.rngMu.Unlock()
	return v
}

func (s *Server) DropMessage(msg *pb.Envelope) error {
	if msg == nil {
		return fmt.Errorf("[DROP ERR] Message is nil")
	}
	log.Printf("[DROP] Dropped: %s -> %s", msg.From, msg.To)
	return nil
}

func (s *Server) DuplicateMessage(msg *pb.Envelope) error {
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
				log.Printf("[DUPE] Duplicated: %s -> %s", m.From, m.To)
			}
		}(target, clone)
		return nil
	}

	// Synchronous duplicate.
	if err := target.SendEnvelope(clone); err != nil {
		return err
	}
	log.Printf("[DUPE] Duplicated: %s -> %s", clone.From, clone.To)
	return nil
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
		log.Printf("[REORD] Delaying: %s -> %s for %v", m.From, m.To, d)
		time.Sleep(d)
		if err := n.SendEnvelope(m); err != nil {
			log.Printf("[REORD ERR] failed send after delay: %v", err)
		} else {
			log.Printf("[REORD] Sent delayed: %s -> %s", m.From, m.To)
		}
	}(target, clone, d)

	return true
}

// ReorderMessage chooses a random delay between ReorderMinDelay and ReorderMaxDelay
// (seconds) and schedules the delayed send.
func (s *Server) ReorderMessage(msg *pb.Envelope) (bool, error) {
	min := s.testConfig.ReorderMinDelay
	max := s.testConfig.ReorderMaxDelay

	if min > max {
		return false, fmt.Errorf("ReorderMinDelay (%d) cannot be greater than ReorderMaxDelay (%d)", min, max)
	}

	var secs int
	if max == min {
		secs = min
	} else {
		secs = s.randIntn(max-min+1) + min
	}
	d := time.Duration(secs) * time.Second
	if scheduled := s.delaySendWithDuration(msg, d); scheduled {
		return true, nil // skip immediate send; delayed goroutine will deliver
	}
	// Destination unknown right now; fall back to immediate send by returning false
	return false, nil
}

// handleMessageEvents processes message events (drop, duplicate, reorder).
// It returns (true, nil) if the message delivery should be skipped (dropped or scheduled for later).
func (s *Server) handleMessageEvents(msg *pb.Envelope) (bool, error) {
	// Do not manipulate messages involving TESTER or the controller itself ("CTRL").
	// Reordering controller-bound traffic can cause unknown-destination during startup/reset.
	if isTesterMsg(msg) || isControlMsg(msg) {
		return false, nil
	}
	
	if s.probCheck(s.testConfig.DropProb) {
		if err := s.DropMessage(msg); err != nil {
			return false, fmt.Errorf("[DROP ERR] %v", err)
		}
		return true, nil
	}

	if s.probCheck(s.testConfig.DupeProb) {
		if err := s.DuplicateMessage(msg); err != nil {
			return false, fmt.Errorf("[DUPE ERR] %v", err)
		}
		return false, nil
	}

	if s.probCheck(s.testConfig.ReorderProb) {
		scheduled, err := s.ReorderMessage(msg)
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
