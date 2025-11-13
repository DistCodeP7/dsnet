package algorithms

import (
	"fmt"
	"testing"
	"time"

	"github.com/distcode/dsnet/dsnet"
	pb "github.com/distcode/dsnet/proto"
	"github.com/distcode/dsnet/testutils"
)

func TestCentralizedMutex(t *testing.T) {
	server, lis := testutils.StartTestServer(t)
	defer server.Stop()

	addr := lis.Addr().String()
	nodes := []string{"A", "B", "C"}
	network := make(map[string]*dsnet.DSNet)

	//Create nodes
	for _, id := range nodes {
		node, err := dsnet.Connect(id, addr)
		if err != nil {
			t.Fatalf("failed to connect node %s: %v", id, err)
		}
		network[id] = node
		defer node.Close()
	}

	//Add mutexHandlers to each node
	mutexHandlers := make(map[string]*MutexHandler)
	for _, id := range nodes {
		hasToken := id == "A"
		mutexHandlers[id] = NewMutexHandler(id, hasToken)
	}

	cs := &testutils.CriticalSection{}

	// Start goroutines to handle incoming messages and CS work
	for _, id := range nodes {
		node := network[id]
		mutex := mutexHandlers[id]

		go func(nodeID string, node *dsnet.DSNet, mutex *MutexHandler) {
			for env := range node.Inbox {
				responses := mutex.OnEvent(env)
				for _, r := range responses {
					_ = node.Send(r.To, r.Payload, r.Type)
				}
			}
		}(id, node, mutex)

		go func(nodeID string, mutex *MutexHandler) {
			for range mutex.csEntryCh {
				cs.Work(nodeID, 200*time.Millisecond, func() {
					fmt.Printf("[%s] simulated work\n", nodeID)
				})
			}

			if next := mutex.NextWaiting(); next != "" {
				_ = network[next].Send(next, "TOKEN", pb.MessageType_REQUEST_TOKEN)
			}
		}(id, mutex)
	}
}
