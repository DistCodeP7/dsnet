package main

import (
	"io"
	"log"
	"net"
	"sync"

	pb "github.com/distcodep7/dsnet/proto"

	"google.golang.org/grpc"
)

type Server struct {
	pb.UnimplementedNetworkControllerServer
	mu    sync.Mutex
	nodes map[string]pb.NetworkController_StreamServer
}

func (s *Server) Stream(stream pb.NetworkController_StreamServer) error {
	// 1. Wait for the first message to identify the node (Handshake)
	firstMsg, err := stream.Recv()
	if err != nil {
		return err
	}

	nodeID := firstMsg.From
	s.mu.Lock()
	s.nodes[nodeID] = stream
	s.mu.Unlock()

	log.Printf("[CTRL] Node Registered: %s", nodeID)

	// 2. Message Loop
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			// Node disconnected
			s.mu.Lock()
			delete(s.nodes, nodeID)
			s.mu.Unlock()
			log.Printf("[CTRL] Node Disconnected: %s", nodeID)
			return nil
		}
		if err != nil {
			return err
		}

		// 3. Observability & Routing
		// This is where you see EVERYTHING happening in the system
		log.Printf("[LOG] %s -> %s [%s]: %s", msg.From, msg.To, msg.Type, msg.Payload)

		s.forward(msg)
	}
}

func (s *Server) forward(msg *pb.Envelope) {
	s.mu.Lock()
	target, ok := s.nodes[msg.To]
	s.mu.Unlock()

	if ok {
		target.Send(msg)
	} else {
		log.Printf("[ERR] Unknown destination: %s", msg.To)
	}
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterNetworkControllerServer(s, &Server{
		nodes: make(map[string]pb.NetworkController_StreamServer),
	})
	log.Println("Controller listening on :50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
