package dsnet

import (
	"testing"

	pb "github.com/distcode/dsnet/proto"
	"github.com/distcode/dsnet/testutils"
)

func TestClientMessaging(t *testing.T) {
	grpcServer, lis := testutils.StartTestServer(t)
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
	if err := nodeA.Send("nodeB", expectedMsgAtoB, pb.MessageType_DIRECT); err != nil {
		t.Fatalf("Failed to send message from A to B: %v", err)
	}

	// Node B sends to Node A
	expectedMsgBtoA := "Hello from B to A"
	if err := nodeB.Send("nodeA", expectedMsgBtoA, pb.MessageType_DIRECT); err != nil {
		t.Fatalf("Failed to send message from B to A: %v", err)
	}

	if !testutils.WaitForMsg(nodeB.Inbox, expectedMsgAtoB, "nodeA") {
		t.Errorf("NodeB did not receive expected message from NodeA")
	}
	if !testutils.WaitForMsg(nodeA.Inbox, expectedMsgBtoA, "nodeB") {
		t.Errorf("NodeA did not receive expected message from NodeB")
	}
}
