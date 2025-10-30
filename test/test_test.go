package dsnet_test

import (
	"context"
	"fmt"
	"net"

	"testing"
	"time"

	"github.com/distcode/dsnet/controller"
	"github.com/distcode/dsnet/dsnet"
	gh "github.com/distcode/dsnet/proto"

	"google.golang.org/grpc"
)

// minimal typed message
type Heartbeat struct {
	Term int
}

func TestMinimalTypeSafeMessaging(t *testing.T) {
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	grpcServer := grpc.NewServer()
	ctrl := controller.NewController(controller.ControllerProps{})
	gh.RegisterNetworkControllerServer(grpcServer, ctrl)
	go grpcServer.Serve(lis)
	defer grpcServer.GracefulStop()
	defer lis.Close()

	nodeA, err := dsnet.Connect("localhost:50052", "nodeA")
	if err != nil {
		t.Fatalf("connect nodeA: %v", err)
	}
	defer nodeA.Close()

	nodeB, err := dsnet.Connect("localhost:50052", "nodeB")
	if err != nil {
		t.Fatalf("connect nodeB: %v", err)
	}
	defer nodeB.Close()

	// --- Register type-safe handler on nodeB ---
	done := make(chan struct{})
	dsnet.On(nodeB, "heartbeat", func(ctx context.Context, from string, msg Heartbeat) error {
		fmt.Printf("NodeB received heartbeat from %s: %+v\n", from, msg.Term)
		close(done)
		return nil
	})

	// --- nodeA sends Heartbeat to nodeB ---
	hb := Heartbeat{Term: 42}
	if err := nodeA.Send("nodeB", hb, "heartbeat"); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// --- Wait for handler to fire ---
	select {
	case <-done:
		fmt.Println("✅ Type-safe message received successfully")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: message not received")
	}

	fmt.Println(ctrl.GetLog())
}
