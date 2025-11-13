package algorithms

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/distcode/dsnet/dsnet"
	pb "github.com/distcode/dsnet/proto"
	"github.com/distcode/dsnet/testutils"
)

func TestCentralizedMutex(t *testing.T) {
	server, lis := testutils.StartTestServer(t)
	defer server.Stop()
	defer lis.Close()

	addr := lis.Addr().String()
	nodes := []string{"A", "B", "C"}
	network := make(map[string]*dsnet.DSNet)
	mutexHandlers := make(map[string]*MutexHandler)

	// Create nodes
	for _, id := range nodes {
		node, err := dsnet.Connect(id, lis.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect node %s: %v", id, err)
		}
		network[id] = node
		defer node.Close()
	}

	// Create mutex handlers
	for _, id := range nodes {
		hasToken := id == "A"
		mutexHandlers[id] = NewMutexHandler(id, hasToken)
	}

	cs := &testutils.CriticalSection{}
	var wg sync.WaitGroup
	// Only B and C will request the token from A in this test, so expect two
	// critical-section entries.
	wg.Add(2)

	for _, id := range nodes {
		node := network[id]
		mutex := mutexHandlers[id]

		// Handle incoming messages
		go func(nodeID string, node *dsnet.DSNet, mutex *MutexHandler) {
			for env := range node.Inbox {
				responses := mutex.OnEvent(env)
				for _, r := range responses {
					_ = node.Send(r.To, r.Payload, r.Type)
				}
			}
		}(id, node, mutex)

		// Handle CS entry
		go func(nodeID string, mutex *MutexHandler) {
			for range mutex.csEntryCh {
				cs.Work(nodeID, 300*time.Millisecond, func() {
					fmt.Printf("[%s] entered critical section\n", nodeID)
				})

				wg.Done() // signal completion

				_ = network[nodeID].Send("A", "TOKEN", pb.MessageType_TOKEN)
				fmt.Printf("[%s] returned token to A\n", nodeID)
			}
		}(id, mutex)
	}

	// Trigger token requests
	go func() {
		time.Sleep(500 * time.Millisecond)
		mutexHandlers["B"].RequestToken(network["B"], "A")
		time.Sleep(500 * time.Millisecond)
		mutexHandlers["C"].RequestToken(network["C"], "A")
	}()

	// Wait for all nodes to enter CS once
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		fmt.Println("All nodes have entered the critical section once")
	case <-time.After(30 * time.Second):
		t.Fatal("Test timed out – likely a deadlock")
	}
}
