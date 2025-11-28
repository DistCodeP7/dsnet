package controller

import (
	"testing"

	pb "github.com/distcodep7/dsnet/proto"
)

// Test that BlockCommunication and UnblockCommunication update the map as expected.
func TestBlockUnblock(t *testing.T) {
	s := &Server{
		nodes:   make(map[string]*Node),
		blocked: make(map[string]map[string]bool),
	}

	a, b := "A", "B"

	// Block A -> B
	s.BlockCommunication(a, b)
	if rules, ok := s.blocked[a]; !ok {
		t.Fatalf("expected blocked entry for %s to exist", a)
	} else if !rules[b] {
		t.Fatalf("expected %s -> %s to be blocked", a, b)
	}

	// Unblock A -> B
	s.UnblockCommunication(a, b)
	if rules, ok := s.blocked[a]; ok {
		if rules[b] {
			t.Fatalf("expected %s -> %s to be unblocked", a, b)
		}
		// If the inner map is empty you may decide to delete it; current implementation leaves it.
		_ = rules
	}
}

// Test CreatePartition blocks all cross links between two groups (both directions).
func TestCreatePartition(t *testing.T) {
	s := &Server{
		nodes:   make(map[string]*Node),
		blocked: make(map[string]map[string]bool),
	}

	g1 := []string{"A", "C"}
	g2 := []string{"B", "D"}

	s.CreatePartition(g1, g2)

	// verify all cross pairs are blocked both directions
	for _, x := range g1 {
		for _, y := range g2 {
			if rules, ok := s.blocked[x]; !ok || !rules[y] {
				t.Fatalf("expected %s -> %s to be blocked by CreatePartition", x, y)
			}
			if rules, ok := s.blocked[y]; !ok || !rules[x] {
				t.Fatalf("expected %s -> %s to be blocked by CreatePartition", y, x)
			}
		}
	}
}

// Test forward returns early (no panic) when the sender -> recipient pair is blocked.
// This ensures Send is not invoked for blocked pairs.
func TestForward_DropsWhenBlocked(t *testing.T) {
	s := &Server{
		nodes:   make(map[string]*Node),
		blocked: make(map[string]map[string]bool),
	}

	// Put a Node entry for the destination (stream left nil intentionally).
	s.nodes["B"] = &Node{id: "B", stream: nil}

	// Block A -> B
	s.BlockCommunication("A", "B")

	env := &pb.Envelope{From: "A", To: "B", Type: "MSG", Payload: "payload"}

	// forward should return without calling Send (and thus not panic despite nil stream)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("forward panicked for blocked pair: %v", r)
		}
	}()
	s.forward(env)
}

// Test forward with unknown destination should not panic even when not blocked.
func TestForward_UnknownDestination(t *testing.T) {
	s := &Server{
		nodes:   make(map[string]*Node),
		blocked: make(map[string]map[string]bool),
	}

	env := &pb.Envelope{From: "X", To: "NonExistent", Type: "MSG", Payload: "payload"}

	// Should not panic when target not present
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("forward panicked for unknown destination: %v", r)
		}
	}()
	s.forward(env)
}
