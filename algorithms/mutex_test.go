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

// combinedHandler routes request/release messages to the MutexServer and TOKEN messages
// to the MutexClient. It implements dsnet.NodeHandler so that DSNet.RunAlgo will
// automatically forward any returned envelopes.
	type combinedHandler struct {
		srv *MutexServer
		cli *MutexClient
	}

	func (h *combinedHandler) OnEvent(env *pb.Envelope) []*pb.Envelope {
		switch env.Type {
		case pb.MessageType_REQUEST_TOKEN, pb.MessageType_RELEASE_TOKEN:
			return h.srv.OnEvent(env)
		case pb.MessageType_TOKEN:
			h.cli.OnEvent(env)
		}
		return nil
	}

func TestCentralizedMutex(t *testing.T) {
	server, lis := testutils.StartTestServer(t)
	defer server.Stop()
	defer lis.Close()

	addr := lis.Addr().String()
	nodes := []string{"A", "B", "C", "D", "E"}
	network := make(map[string]*dsnet.DSNet)
	mutexClients := make(map[string]*MutexClient)

	// Create nodes
	for _, id := range nodes {
		node, err := dsnet.Connect(id, addr)
		if err != nil {
			t.Fatalf("failed to connect node %s: %v", id, err)
		}
		network[id] = node
		defer node.Close()
	}

	// Create mutex clients and a single central server (on A)
	var centralServer *MutexServer
	for _, id := range nodes {
		hasToken := id == "A"
		mutexClients[id] = &MutexClient{
			NodeID:    id,
			hasToken:  hasToken,
			csEntryCh: make(chan struct{}, 1),
		}
		if id == "A" {
			centralServer = NewMutexServer(id, hasToken)
		}
	}

	cs := &testutils.CriticalSection{}
	var wg sync.WaitGroup
	wg.Add(4)

	for _, id := range nodes {
		node := network[id]
		mutex := mutexClients[id]

		// Combined handler: route request/release to the central server (only present on A)
		// and route TOKEN messages to the client so it can enter the CS.
		var srv *MutexServer
		if id == "A" {
			srv = centralServer
		}
		go node.RunAlgo(&combinedHandler{srv: srv, cli: mutex})

		// Handle CS entry
		go func(nodeID string, mutex *MutexClient) {
			for range mutex.csEntryCh {
				cs.Work(nodeID, 300*time.Millisecond, func() {
					fmt.Printf("[%s] entered critical section\n", nodeID)
				})
				wg.Done() // signal completion

				// Release token back to A
				mutex.ReleaseToken(network[nodeID], "A")
				fmt.Printf("[%s] returned token to A\n", nodeID)
			}
		}(id, mutex)
	}

	// Trigger token requests
	go func() {
		mutexClients["B"].RequestToken(network["B"], "A")
		mutexClients["C"].RequestToken(network["C"], "A")
		mutexClients["D"].RequestToken(network["D"], "A")
		mutexClients["E"].RequestToken(network["E"], "A")
	}()

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		fmt.Println("All nodes have entered the critical section once")
	case <-time.After(30 * time.Second):
		t.Fatal("Test timed out, likely a deadlock")
	}
}
