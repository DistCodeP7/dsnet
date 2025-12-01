package predicates

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/distcodep7/dsnet/dsnet"
	"github.com/distcodep7/dsnet/exercises/leader_election"
	testing "github.com/distcodep7/dsnet/testing/harness"
)

// ElectionResult describes the harness-evaluated election outcome.
type ElectionResult struct {
	Success  bool
	Reason   string
	LeaderID string
}

// RunLeaderElection triggers an election (optionally to all nodes) and verifies a single leader emerges.
// totalNodes is cluster size; majority = floor(n/2)+1
func RunLeaderElection(ctx context.Context, h *testing.Harness, electionID string, totalNodes int, timeout time.Duration, triggerAll bool) (*ElectionResult, error) {
	majority := (totalNodes / 2) + 1

	// 1) Inject ElectionTrigger to either N1 or all nodes depending on triggerAll
	if triggerAll {
		for i := 1; i <= totalNodes; i++ {
			node := fmt.Sprintf("N%d", i)
			tr := leader_election.ElectionTrigger{
				BaseMessage: dsnet.BaseMessage{From: "TESTER", To: node, Type: "ElectionTrigger"},
				ElectionID:  electionID,
			}
			if err := h.Inject(ctx, "TESTER", node, "ElectionTrigger", tr, 0); err != nil {
				return nil, fmt.Errorf("inject trigger: %w", err)
			}
		}
	} else {
		if err := h.Inject(ctx, "TESTER", "N1", "ElectionTrigger", leader_election.ElectionTrigger{
			BaseMessage: dsnet.BaseMessage{From: "TESTER", To: "N1", Type: "ElectionTrigger"},
			ElectionID:  electionID,
		}, 0); err != nil {
			return nil, fmt.Errorf("inject trigger: %w", err)
		}
	}

	deadline := time.Now().Add(timeout)
	// mapping candidate -> granted votes set
	votes := map[string]map[string]struct{}{}

	for time.Now().Before(deadline) {
		trace := h.SnapshotTrace()
		for _, o := range trace {
			if o.Kind != "Forward" || o.Type != "VoteResponse" && o.Type != "RequestVote" {
				continue
			}
			// parse VoteResponse
			if o.Type == "VoteResponse" {
				var vr leader_election.VoteResponse
				if err := json.Unmarshal(o.Payload, &vr); err != nil {
					continue
				}
				if !vr.Granted {
					continue
				}
				// vr.From is voter; vr.To is candidate
				candidate := vr.To
				voter := vr.From
				if votes[candidate] == nil {
					votes[candidate] = make(map[string]struct{})
				}
				votes[candidate][voter] = struct{}{}
			}
		}
		// check for any candidate with majority
		var winner string
		var winnerCount int
		for cand, set := range votes {
			if len(set) >= majority {
				if winner != "" && winner != cand {
					// two winners -> protocol violation
					return &ElectionResult{
						Success: false,
						Reason:  fmt.Sprintf("two nodes have majority: %s and %s", winner, cand),
					}, nil
				}
				winner = cand
				winnerCount = len(set)
			}
		}
		if winner != "" {
			return &ElectionResult{
				Success:  true,
				LeaderID: winner,
				Reason:   fmt.Sprintf("leader %s with %d votes", winner, winnerCount),
			}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return &ElectionResult{
		Success: false,
		Reason:  "timed out waiting for |majority| votes",
	}, nil
}
