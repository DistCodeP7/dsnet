package dsnet

import (
	"gotest/controller"
	gh "gotest/dsnet/gotest/dsnet/proto"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func startTestServer(t *testing.T) (*grpc.Server, net.Listener) {
	grpcServer := grpc.NewServer()
	ctrl := controller.NewController(controller.ControllerProps{})
	gh.RegisterNetworkControllerServer(grpcServer, ctrl)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	go grpcServer.Serve(lis)

	return grpcServer, lis
}

func waitForMsg(ch chan *gh.Envelope, expected, from string) bool {
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			if msg.Payload == expected && msg.From == from {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

func TestClientMessaging(t *testing.T) {
	grpcServer, lis := startTestServer(t)
	defer func() {
		grpcServer.GracefulStop()
		lis.Close()
	}()

	nodeA, err := Connect("localhost:50051", "nodeA")
	if err != nil {
		t.Fatalf("Failed to connect nodeA: %v", err)
	}
	nodeB, err := Connect("localhost:50051", "nodeB")
	if err != nil {
		t.Fatalf("Failed to connect nodeB: %v", err)
	}

	// Ensure clients are properly closed after test
	defer func() {
		nodeA.Close()
		nodeB.Close()
	}()

	// Node A sends to Node B
	expectedMsgAtoB := "Hello from A to B"
	if err := nodeA.Send("nodeB", expectedMsgAtoB); err != nil {
		t.Fatalf("Failed to send message from A to B: %v", err)
	}

	// Node B sends to Node A
	expectedMsgBtoA := "Hello from B to A"
	if err := nodeB.Send("nodeA", expectedMsgBtoA); err != nil {
		t.Fatalf("Failed to send message from B to A: %v", err)
	}

	if !waitForMsg(nodeB.Inbox, expectedMsgAtoB, "nodeA") {
		t.Errorf("NodeB did not receive expected message from NodeA")
	}
	if !waitForMsg(nodeA.Inbox, expectedMsgBtoA, "nodeB") {
		t.Errorf("NodeA did not receive expected message from NodeB")
	}
}

func TestBroadcastingMessage(t *testing.T) {
	grpcServer, lis := startTestServer(t)
	defer func() {
		grpcServer.GracefulStop()
		lis.Close()
	}()

	nodeA, err := Connect("localhost:50051", "nodeA")
	if err != nil {
		t.Fatalf("Failed to connect nodeA: %v", err)
	}
	nodeB, err := Connect("localhost:50051", "nodeB")
	if err != nil {
		t.Fatalf("Failed to connect nodeB: %v", err)
	}

	defer func() {
		nodeA.Close()
		nodeB.Close()
	}()

	expectedMsgAtoB := "Hello from A to B"
	if err := nodeA.Broadcast(expectedMsgAtoB); err != nil {
		t.Fatalf("Failed to broadcast from A: %v", err)
	}

	expectedMsgBtoA := "Hello from B to A"
	if err := nodeB.Broadcast(expectedMsgBtoA); err != nil {
		t.Fatalf("Failed to broadcast from B: %v", err)
	}

	if !waitForMsg(nodeB.Inbox, expectedMsgAtoB, "nodeA") {
		t.Errorf("NodeB did not receive expected message from NodeA")
	}
	if !waitForMsg(nodeA.Inbox, expectedMsgBtoA, "nodeB") {
		t.Errorf("NodeA did not receive expected message from NodeB")
	}
}

func TestBroadcastingMessageThreeNodes(t *testing.T) {
	grpcServer, lis := startTestServer(t)
	defer func() {
		grpcServer.GracefulStop()
		lis.Close()
	}()

	// Connect three nodes
	nodeA, err := Connect("localhost:50051", "nodeA")
	if err != nil {
		t.Fatalf("Failed to connect nodeA: %v", err)
	}
	nodeB, err := Connect("localhost:50051", "nodeB")
	if err != nil {
		t.Fatalf("Failed to connect nodeB: %v", err)
	}
	nodeC, err := Connect("localhost:50051", "nodeC")
	if err != nil {
		t.Fatalf("Failed to connect nodeC: %v", err)
	}

	defer func() {
		nodeA.Close()
		nodeB.Close()
		nodeC.Close()
	}()

	// Broadcast messages concurrently
	msgA := "Hello from A"
	msgB := "Hello from B"

	if err := nodeA.Broadcast(msgA); err != nil {
		t.Fatalf("Failed to broadcast from nodeA: %v", err)
	}
	if err := nodeB.Broadcast(msgB); err != nil {
		t.Fatalf("Failed to broadcast from nodeB: %v", err)
	}

	// Helper function to collect all messages received within 2 seconds
	collectMessages := func(ch chan *gh.Envelope, expectedCount int) map[string]string {
		received := make(map[string]string)
		timeout := time.After(2 * time.Second)
		for len(received) < expectedCount {
			select {
			case msg := <-ch:
				received[msg.Payload] = msg.From
			case <-timeout:
				return received
			}
		}
		return received
	}

	// NodeC should receive both broadcasts
	receivedC := collectMessages(nodeC.Inbox, 2)
	if from, ok := receivedC[msgA]; !ok || from != "nodeA" {
		t.Errorf("NodeC did not receive broadcast from nodeA")
	}
	if from, ok := receivedC[msgB]; !ok || from != "nodeB" {
		t.Errorf("NodeC did not receive broadcast from nodeB")
	}

	// NodeA should receive broadcast from nodeB
	receivedA := collectMessages(nodeA.Inbox, 2)
	if from, ok := receivedA[msgA]; !ok || from != "nodeA" {
		t.Errorf("NodeA did not receive broadcast from nodeA")
	}
	if from, ok := receivedA[msgB]; !ok || from != "nodeB" {
		t.Errorf("NodeA did not receive broadcast from nodeB")
	}

	// NodeB should receive broadcast from nodeA
	receivedB := collectMessages(nodeB.Inbox, 2)
	if from, ok := receivedB[msgA]; !ok || from != "nodeA" {
		t.Errorf("NodeB did not receive broadcast from nodeA")
	}
	if from, ok := receivedB[msgB]; !ok || from != "nodeB" {
		t.Errorf("NodeB did not receive broadcast from nodeB")
	}
}

func TestGroupMessagingConcurrent(t *testing.T) {
	grpcServer, lis := startTestServer(t)
	defer func() {
		grpcServer.GracefulStop()
		lis.Close()
	}()

	// Connect three nodes
	nodeA, _ := Connect("localhost:50051", "nodeA")
	nodeB, _ := Connect("localhost:50051", "nodeB")
	nodeC, _ := Connect("localhost:50051", "nodeC")
	defer nodeA.Close()
	defer nodeB.Close()
	defer nodeC.Close()

	// Subscribe nodes to groups
	nodeA.Subscribe("AB")
	nodeB.Subscribe("AB")
	nodeB.Subscribe("BC")
	nodeC.Subscribe("BC")
	nodeA.Subscribe("AC")
	nodeC.Subscribe("AC")

	time.Sleep(50 * time.Millisecond) // allow subscriptions to be processed

	// Publish messages to each group
	nodeA.Publish("AB", "msg1-AB")
	nodeB.Publish("BC", "msg2-BC")
	nodeC.Publish("AC", "msg3-AC")

	// Concurrently collect messages for all nodes
	nodes := []*DSNet{nodeA, nodeB, nodeC}
	results := make(map[string]map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)
		go func(n *DSNet) {
			defer wg.Done()
			received := make(map[string]string)
			timeout := time.After(500 * time.Millisecond)
			for {
				select {
				case msg := <-n.Inbox:
					received[msg.Payload] = msg.From
				case <-timeout:
					mu.Lock()
					results[n.NodeID] = received
					mu.Unlock()
					return
				}
			}
		}(node)
	}

	wg.Wait() // wait for all message collectors

	// Assertions: ensure each node received exactly the messages it should
	// Group AB: nodeA and nodeB
	if _, ok := results["nodeA"]["msg1-AB"]; !ok {
		t.Errorf("nodeA did not receive msg1-AB")
	}
	if _, ok := results["nodeB"]["msg1-AB"]; !ok {
		t.Errorf("nodeB did not receive msg1-AB")
	}
	if _, ok := results["nodeC"]["msg1-AB"]; ok {
		t.Errorf("nodeC should not receive AB messages")
	}

	// Group BC: nodeB and nodeC
	if _, ok := results["nodeB"]["msg2-BC"]; !ok {
		t.Errorf("nodeB did not receive msg2-BC")
	}
	if _, ok := results["nodeC"]["msg2-BC"]; !ok {
		t.Errorf("nodeC did not receive msg2-BC")
	}
	if _, ok := results["nodeA"]["msg2-BC"]; ok {
		t.Errorf("nodeA should not receive BC messages")
	}

	// Group AC: nodeA and nodeC
	if _, ok := results["nodeA"]["msg3-AC"]; !ok {
		t.Errorf("nodeA did not receive msg3-AC")
	}
	if _, ok := results["nodeC"]["msg3-AC"]; !ok {
		t.Errorf("nodeC did not receive msg3-AC")
	}
	if _, ok := results["nodeB"]["msg3-AC"]; ok {
		t.Errorf("nodeB should not receive AC messages")
	}
}
