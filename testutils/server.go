package testutils

import (
	"log"
	"net"
	"testing"
	"time"

	"github.com/distcode/dsnet/controller"
	pb "github.com/distcode/dsnet/proto"

	"google.golang.org/grpc"
)

func StartTestServer(t *testing.T) (*grpc.Server, net.Listener) {
	grpcServer := grpc.NewServer()
	ctrl := controller.NewController(controller.ControllerProps{})
	pb.RegisterNetworkControllerServer(grpcServer, ctrl)

	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	
	go func() {
        if err := grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
            log.Printf("gRPC server failed: %v", err)
        }
    }()

	return grpcServer, lis
}

func WaitForMsg(ch chan *pb.Envelope, expected, from string) bool {
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

func SimulateWork(nodeID string) {

}
