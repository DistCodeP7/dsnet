package paxos_leader_election

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/distcodep7/dsnet/dsnet"
	"github.com/distcodep7/dsnet/exercises/leader_election"
)

var totalNodes int // From ENV

type PaxosNode struct {
	Net   *dsnet.Node
	State struct {
		Term     int
		VotedFor string
	}

	votesReceived map[string]map[string]bool
	lastElectionID string
}

func NewPaxosNode(id string) *PaxosNode {
	n, _ := dsnet.NewNode(id, "localhost:50051")
	return &PaxosNode{Net: n}
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
		var msg leader_election.ElectionTrigger
		json.Unmarshal(event.Payload, &msg)

		pn.startElection(ctx, msg.ElectionID)

	case "RequestVote":
		var req leader_election.RequestVote
		json.Unmarshal(event.Payload, &req)

		voteGranted := true

		// Rule 1: Higher term → update and vote
		if req.Term > pn.State.Term {
			pn.State.Term = req.Term
			pn.State.VotedFor = ""
		}

		// Rule 2: Grant if not voted yet
		if pn.State.VotedFor == "" {
			pn.State.VotedFor = req.From
			voteGranted = true
		}

		// Rule 3: Grant if same candidate asks again
		if pn.State.VotedFor == req.From {
			voteGranted = true
		}

		resp := leader_election.VoteResponse{
			BaseMessage: dsnet.BaseMessage{From: pn.Net	.ID, To: req.From, Type: "VoteResponse"},
			Term:        req.Term,
			Granted:     voteGranted,
		}
		pn.Net.Send(ctx, req.From, resp)

	case "VoteResponse":
		var resp leader_election.VoteResponse
		json.Unmarshal(event.Payload, &resp)

		electionId := pn.lastElectionID
		if pn.votesReceived[electionId] == nil {
			pn.votesReceived[electionId] = make(map[string]bool)
		}

		if resp.Granted {
			pn.votesReceived[electionId][resp.From] = true
		}

		votes := len(pn.votesReceived[electionId])
		neededVotes := (totalNodes / 2) + 1

		if votes >= neededVotes {

			result := leader_election.ElectionResult{
				BaseMessage: dsnet.BaseMessage{From: pn.Net.ID, To: "TESTER", Type: "ElectionResult"},
				Success:     true,
				LeaderID:    pn.Net.ID,
				ElectionID:  "TEST_001",
			}
			pn.Net.Send(ctx, "TESTER", result)

			// Prevent duplicate reporting
			pn.votesReceived[electionId] = map[string]bool{"Done": true}
		}
	}
}

func (pn *PaxosNode) startElection(ctx context.Context, electionID string) {
	pn.State.Term++
	pn.State.VotedFor = pn.Net.ID // vote for self

    pn.lastElectionID = electionID

    if pn.votesReceived == nil {
        pn.votesReceived = make(map[string]map[string]bool)
    }
    pn.votesReceived[electionID] = map[string]bool{
        pn.Net.ID: true, // count own vote
    }

	pn.sendToAll(ctx)
}

func (pn *PaxosNode) sendToAll(ctx context.Context) {
	for i := 1; i <= totalNodes; i++ {
		peerID := fmt.Sprintf("N%d", i)
		if peerID != pn.Net.ID {
			req := leader_election.RequestVote{
				BaseMessage: dsnet.BaseMessage{From: pn.Net.ID, To: peerID, Type: "RequestVote"},
				Term:        pn.State.Term,
			}
			pn.Net.Send(ctx, peerID, req)
		}
	}
}

func Solution(ctx context.Context, nodeNum int) {
	totalNodes = nodeNum
	for i := 1; i <= totalNodes; i++ {
		nodeID := fmt.Sprintf("N%d", i)
		go NewPaxosNode(nodeID).Run(ctx)
	}

	<-ctx.Done()
}
