package dsnet

import (
	"net"
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

	nodeA, err := Connect("nodeA")
	if err != nil {
		t.Fatalf("Failed to connect nodeA: %v", err)
	}
	nodeB, err := Connect("nodeB")
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