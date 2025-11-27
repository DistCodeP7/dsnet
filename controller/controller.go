package controller

import (
	"io"
	"log"
	"net"
	"sync"

	pb "github.com/distcodep7/dsnet/proto"

	"google.golang.org/grpc"
)

type Node struct {
	id     string
	stream pb.NetworkController_StreamServer
}

type Server struct {
	pb.UnimplementedNetworkControllerServer

	mu      sync.Mutex
	nodes   map[string]*Node
	blocked map[string]map[string]bool // blocked[A][B] = true means A cannot send to B
}

func (s *Server) Stream(stream pb.NetworkController_StreamServer) error {
	firstMsg, err := stream.Recv()
	if err != nil {
		return err
	}

	nodeID := firstMsg.From

	s.mu.Lock()
	s.nodes[nodeID] = &Node{
		id:     nodeID,
		stream: stream,
	}
	s.mu.Unlock()

	log.Printf("[CTRL] Node Registered: %s", nodeID)

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			s.removeNode(nodeID)
			return nil
		}
		if err != nil {
			s.removeNode(nodeID)
			return err
		}

		log.Printf("[LOG] %s -> %s [%s]: %s", msg.From, msg.To, msg.Type, msg.Payload)

		s.forward(msg)
	}
}

func (s *Server) forward(msg *pb.Envelope) {
	s.mu.Lock()

	if blockedTargets, exists := s.blocked[msg.From]; exists {
		if blockedTargets[msg.To] {
			log.Printf("[PARTITION] Dropped: %s -> %s", msg.From, msg.To)
			s.mu.Unlock()
			return
		}
	}

	target, ok := s.nodes[msg.To]
	s.mu.Unlock()

	if ok {
		target.stream.Send(msg)
	} else {
		log.Printf("[ERR] Unknown destination: %s", msg.To)
	}
}

func (s *Server) removeNode(id string) {
	s.mu.Lock()
	delete(s.nodes, id)
	s.mu.Unlock()
	log.Printf("[CTRL] Node Disconnected: %s", id)
}

func (s *Server) BlockCommunication(a, b string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.blocked[a]; !exists {
		s.blocked[a] = make(map[string]bool)
	}
	s.blocked[a][b] = true
	log.Printf("[PARTITION] Blocked: %s -> %s", a, b)
}

func (s *Server) UnblockCommunication(a, b string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rules, exists := s.blocked[a]; exists {
		delete(rules, b)
		log.Printf("[PARTITION] Unblocked: %s -> %s", a, b)
	}
}

func (s *Server) CreatePartition(group1, group2 []string) {
	for _, a := range group1 {
		for _, b := range group2 {
			s.BlockCommunication(a, b)
			s.BlockCommunication(b, a)
		}
	}
}

func Serve() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := &Server{
		nodes:   make(map[string]*Node),
		blocked: make(map[string]map[string]bool),
	}

	grpcServer := grpc.NewServer()
	pb.RegisterNetworkControllerServer(grpcServer, srv)

	log.Println("Controller listening on :50051...")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
