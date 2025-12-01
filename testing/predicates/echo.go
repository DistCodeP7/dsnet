package predicates

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/distcodep7/dsnet/dsnet"
	"github.com/distcodep7/dsnet/exercises/echo"
	testing "github.com/distcodep7/dsnet/testing/harness"
)

// EchoResult is the outcome of the echo verification.
type EchoResult struct {
	Success bool
	Reason  string
}

// RunEchoTest injects a SendTrigger to `initiator` and checks the trace for correctness.
// totalNodes is the expected cluster size.
func RunEchoTest(ctx context.Context, h *testing.Harness, initiator string, echoID string, content string, totalNodes int, timeout time.Duration) (*EchoResult, error) {
	// 1) Inject the SendTrigger as external input from TESTER to initiator
	trigger := echo.SendTrigger{
		BaseMessage: dsnet.BaseMessage{From: "TESTER", To: initiator, Type: "SendTrigger"}, 
		EchoID:      echoID,
		Content:     content,
	}
	if err := h.Inject(ctx, "TESTER", initiator, "SendTrigger", trigger, 0); err != nil {
		return nil, fmt.Errorf("inject trigger: %w", err)
	}

	// 2) Wait for expected events to appear: initiator -> others EchoMessage; others -> initiator EchoResponse
	deadline := time.Now().Add(timeout)
	// Mapping to record first unique reply from each node
	replySeen := make(map[string]bool)

	for time.Now().Before(deadline) {
		trace := h.SnapshotTrace()
		// find all EchoMessage from initiator
		for _, o := range trace {
			if o.Kind != "Forward" || o.Type != "EchoMessage" {
				continue
			}
			if o.From == initiator {
				// ok, message broadcasted
			}
			// collect responses to initiator
			if o.Type == "EchoResponse" && o.To == initiator {
				// parse payload to get EchoID and sender
				var resp echo.EchoResponse
				if err := json.Unmarshal(o.Payload, &resp); err != nil {
					continue
				}
				if resp.EchoID != echoID {
					continue
				}
				// record unique from nodes (ignore initiator)
				if resp.From == initiator {
					continue
				}
				replySeen[resp.From] = true
			}
		}
		if len(replySeen) >= totalNodes-1 {
			// success
			return &EchoResult{Success: true}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}

	// timed out — build reason
	missing := make([]string, 0)
	for i := 1; i <= totalNodes; i++ {
		node := fmt.Sprintf("N%d", i)
		if node == initiator {
			continue
		}
		if !replySeen[node] {
			missing = append(missing, node)
		}
	}
	reason := fmt.Sprintf("timed out; missing responses from: %v", missing)
	return &EchoResult{Success: false, Reason: reason}, nil
}
