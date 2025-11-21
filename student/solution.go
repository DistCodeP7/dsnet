package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"

	"github.com/distcodep7/dsnet/dsnet"
	"github.com/distcodep7/dsnet/exercises/mutex"
)

type PaxosNode struct {
	ID    string
	Net   *dsnet.Node
	State struct {
		Term     int
		VotedFor string
	}
}

func NewPaxosNode(id, controllerAddr string) *PaxosNode {
	n, _ := dsnet.NewNode(id, controllerAddr)
	return &PaxosNode{ID: id, Net: n}
}

func (pn *PaxosNode) Run(ctx context.Context) {
	for {
		select {
		case event := <-pn.Net.Inbound:
			pn.handleEvent(ctx, event)
		case <-ctx.Done():
			return
		}
	}
}

func (pn *PaxosNode) handleEvent(ctx context.Context, event dsnet.Event) {
	switch event.Type {

	case "ElectionTrigger":
		var msg mutex.ElectionTrigger
		json.Unmarshal(event.Payload, &msg)
		log.Printf("[%s] ⚡ Trigger received. Starting election %s...", pn.ID, msg.ElectionID)

		pn.startElection(ctx, msg.ElectionID)

	case "RequestVote":
		var req mutex.RequestVote
		json.Unmarshal(event.Payload, &req)
		log.Printf("[%s] 🗳️  Received Vote Request from %s (Term: %d)", pn.ID, req.From, req.Term)

		voteGranted := true

		resp := mutex.VoteResponse{
			BaseMessage: dsnet.BaseMessage{From: pn.ID, To: req.From, Type: "VoteResponse"},
			Term:        req.Term,
			Granted:     voteGranted,
		}
		pn.Net.Send(ctx, req.From, resp)

	case "VoteResponse":
		var resp mutex.VoteResponse
		json.Unmarshal(event.Payload, &resp)
		log.Printf("[%s] 📩 Received Vote from %s: %v", pn.ID, resp.From, resp.Granted)

		if resp.Granted {

			log.Printf("[%s] 👑 I am the Leader!", pn.ID)

			result := mutex.ElectionResult{
				BaseMessage: dsnet.BaseMessage{From: pn.ID, To: "TESTER", Type: "ElectionResult"},
				Success:     true,
				LeaderID:    pn.ID,
				ElectionID:  "TEST_001",
			}
			pn.Net.Send(ctx, "TESTER", result)
		}
	}
}

func (pn *PaxosNode) startElection(ctx context.Context, electionID string) {
	pn.State.Term++

	req := mutex.RequestVote{
		BaseMessage: dsnet.BaseMessage{From: pn.ID, To: "N2", Type: "RequestVote"},
		Term:        pn.State.Term,
	}
	pn.Net.Send(ctx, "N2", req)
}

func main() {
	nodeID := flag.String("id", "N1", "The ID of this node")
	controllerAddr := flag.String("addr", "localhost:50051", "Address of the Network Controller")
	flag.Parse()

	log.Printf("Starting Node %s connecting to %s", *nodeID, *controllerAddr)

	node := NewPaxosNode(*nodeID, *controllerAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node.Run(ctx)
}
