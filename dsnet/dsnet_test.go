package dsnet

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/distcode/dsnet/controller"
	gh "github.com/distcode/dsnet/proto"

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

// helper to check payload
func payloadEquals(env *gh.Envelope, msg, from string) bool {
	if env.From != from || env.Payload == nil {
		return false
	}
	v, ok := env.Payload.Fields["message"]
	if !ok {
		return false
	}
	return v.GetStringValue() == msg
}

func waitForMsg(ch chan *gh.Envelope, expected, from string) bool {
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg := <-ch:
			if payloadEquals(msg, expected, from) {
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
	defer nodeA.Close()
	defer nodeB.Close()

	expectedMsgAtoB := "Hello from A to B"
	if err := nodeA.Send("nodeB", map[string]any{"message": expectedMsgAtoB}, "chat"); err != nil {
		t.Fatalf("Failed to send message from A to B: %v", err)
	}

	expectedMsgBtoA := "Hello from B to A"
	if err := nodeB.Send("nodeA", map[string]any{"message": expectedMsgBtoA}, "chat"); err != nil {
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

	nodeA, _ := Connect("localhost:50051", "nodeA")
	nodeB, _ := Connect("localhost:50051", "nodeB")
	defer nodeA.Close()
	defer nodeB.Close()

	if err := nodeA.Broadcast(map[string]any{"message": "Hello from A to B"}, "chat"); err != nil {
		t.Fatalf("Failed to broadcast from A: %v", err)
	}
	if err := nodeB.Broadcast(map[string]any{"message": "Hello from B to A"}, "chat"); err != nil {
		t.Fatalf("Failed to broadcast from B: %v", err)
	}

	if !waitForMsg(nodeB.Inbox, "Hello from A to B", "nodeA") {
		t.Errorf("NodeB did not receive expected message from NodeA")
	}
	if !waitForMsg(nodeA.Inbox, "Hello from B to A", "nodeB") {
		t.Errorf("NodeA did not receive expected message from NodeB")
	}
}

func TestBroadcastingMessageThreeNodes(t *testing.T) {
	grpcServer, lis := startTestServer(t)
	defer func() {
		grpcServer.GracefulStop()
		lis.Close()
	}()

	nodeA, _ := Connect("localhost:50051", "nodeA")
	nodeB, _ := Connect("localhost:50051", "nodeB")
	nodeC, _ := Connect("localhost:50051", "nodeC")
	defer nodeA.Close()
	defer nodeB.Close()
	defer nodeC.Close()

	if err := nodeA.Broadcast(map[string]any{"message": "Hello from A"}, "chat"); err != nil {
		t.Fatalf("Failed to broadcast from nodeA: %v", err)
	}
	if err := nodeB.Broadcast(map[string]any{"message": "Hello from B"}, "chat"); err != nil {
		t.Fatalf("Failed to broadcast from nodeB: %v", err)
	}

	// collect messages helper
	collectMessages := func(ch chan *gh.Envelope, expectedCount int) map[string]string {
		received := make(map[string]string)
		timeout := time.After(2 * time.Second)
		for len(received) < expectedCount {
			select {
			case msg := <-ch:
				msgStr := msg.Payload.Fields["message"].GetStringValue()
				received[msgStr] = msg.From
			case <-timeout:
				return received
			}
		}
		return received
	}

	receivedC := collectMessages(nodeC.Inbox, 2)
	if from, ok := receivedC["Hello from A"]; !ok || from != "nodeA" {
		t.Errorf("NodeC did not receive broadcast from nodeA")
	}
	if from, ok := receivedC["Hello from B"]; !ok || from != "nodeB" {
		t.Errorf("NodeC did not receive broadcast from nodeB")
	}
}

func TestGroupMessagingConcurrent(t *testing.T) {
	grpcServer, lis := startTestServer(t)
	defer func() {
		grpcServer.GracefulStop()
		lis.Close()
	}()

	nodeA, _ := Connect("localhost:50051", "nodeA")
	nodeB, _ := Connect("localhost:50051", "nodeB")
	nodeC, _ := Connect("localhost:50051", "nodeC")
	defer nodeA.Close()
	defer nodeB.Close()
	defer nodeC.Close()

	nodeA.Subscribe("AB")
	nodeB.Subscribe("AB")
	nodeB.Subscribe("BC")
	nodeC.Subscribe("BC")
	nodeA.Subscribe("AC")
	nodeC.Subscribe("AC")

	time.Sleep(50 * time.Millisecond)

	nodeA.Publish("AB", map[string]any{"message": "msg1-AB"}, "group_msg")
	nodeB.Publish("BC", map[string]any{"message": "msg2-BC"}, "group_msg")
	nodeC.Publish("AC", map[string]any{"message": "msg3-AC"}, "group_msg")

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
					received[msg.Payload.Fields["message"].GetStringValue()] = msg.From
				case <-timeout:
					mu.Lock()
					results[n.nodeId] = received
					mu.Unlock()
					return
				}
			}
		}(node)
	}

	wg.Wait()

	// Assertions for groups
	if _, ok := results["nodeA"]["msg1-AB"]; !ok {
		t.Errorf("nodeA did not receive msg1-AB")
	}
	if _, ok := results["nodeB"]["msg1-AB"]; !ok {
		t.Errorf("nodeB did not receive msg1-AB")
	}
	if _, ok := results["nodeC"]["msg1-AB"]; ok {
		t.Errorf("nodeC should not receive AB messages")
	}

	if _, ok := results["nodeB"]["msg2-BC"]; !ok {
		t.Errorf("nodeB did not receive msg2-BC")
	}
	if _, ok := results["nodeC"]["msg2-BC"]; !ok {
		t.Errorf("nodeC did not receive msg2-BC")
	}
	if _, ok := results["nodeA"]["msg2-BC"]; ok {
		t.Errorf("nodeA should not receive BC messages")
	}

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
