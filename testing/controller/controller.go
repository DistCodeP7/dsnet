package controller

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/distcodep7/dsnet/proto"
	"google.golang.org/grpc"
)

// sender interface for testing/mocking message sending.
type sender interface {
	SendEnvelope(*pb.Envelope) error
}

type Node struct {
	id     string
	stream pb.NetworkController_StreamServer
	sendMu sync.Mutex
	alive  atomic.Bool
}

type TestConfig struct {
	DropProb       	float64
	DupeProb        float64
	AsyncDuplicate 	bool
	ReorderProb		float64
	ReorderMinDelay int
	ReorderMaxDelay int
}

type ServerConfig struct {
	testCfg TestConfig
	addr string
	rng  *rand.Rand
}

type Server struct {
	pb.UnimplementedNetworkControllerServer

	mu      	sync.Mutex
	nodes   	map[string]*Node
	senders 	map[string]sender
	blocked 	map[string]map[string]bool // blocked[A][B] = true means A cannot send to B
	rng     	*rand.Rand
	rngMu		sync.Mutex
	testConfig  TestConfig
}

func (sc ServerConfig) WithTestConfig(tc TestConfig) ServerConfig {
	sc.testCfg = tc
	return sc
}

func NewTestConfig() TestConfig {
	return TestConfig{
		DropProb:       	0,
		ReorderProb:  		0,
		DupeProb:       	0,
		AsyncDuplicate: 	true,
		ReorderMinDelay: 	0,
		ReorderMaxDelay: 	0,
	}
}

func NewServerConfig(addr string, testCfg TestConfig) ServerConfig {
	return ServerConfig{
		addr:    addr,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
		testCfg: testCfg,
	}
}

func NewServer(cfg ServerConfig) *Server {
	return &Server{
		nodes:      make(map[string]*Node),
		senders:    make(map[string]sender),
		blocked:    make(map[string]map[string]bool),
		rng:        cfg.rng,
		testConfig: cfg.testCfg,
	}
}

func (n *Node) send(env *pb.Envelope) error {
	if !n.alive.Load() {
		log.Printf("[LOG] Message lost due to dead receiver node: %s -> %s", env.From, env.To)
		return nil
	}
	
	n.sendMu.Lock()
	err := n.stream.Send(env)
	n.sendMu.Unlock()

	return err
}

func (s *Server) Stream(stream pb.NetworkController_StreamServer) error {
	firstMsg, err := stream.Recv()
	if err != nil {
		return err
	}

	nodeID := firstMsg.From

	s.mu.Lock()
	n := &Node{
		id:     nodeID,
		stream: stream,
	}
	n.alive.Store(true)
	s.nodes[nodeID] = n
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

func (n *Node) SendEnvelope(env *pb.Envelope) error {
	if n.stream == nil {
		return fmt.Errorf("Node stream not initialized")
	}
	n.sendMu.Lock()
	defer n.sendMu.Unlock()
	return n.stream.Send(env)
}

func (s *Server) forward(msg *pb.Envelope) {
	s.mu.Lock()

	//Partition
	if blockedTargets, exists := s.blocked[msg.From]; exists {
		if blockedTargets[msg.To] {
			log.Printf("[PARTITION] Dropped: %s -> %s", msg.From, msg.To)
			s.mu.Unlock()
			return
		}
	}

	target, ok := s.nodes[msg.To]
	s.mu.Unlock()

	if !ok {
		log.Printf("[ERR] Unknown destination: %s", msg.To)
		return
	}

	skippedMessage, err := s.handleMessageEvents(msg)
	if err != nil {
		log.Printf("[EVNT ERR] %v", err)
	}
	if skippedMessage {
		// message delivery intentionally skipped (dropped or scheduled)
		return
	}

	if err := target.send(msg); err != nil {
		log.Printf("[ERR] send failed: %v", err)
	}
}

func (s *Server) removeNode(id string) {
	s.mu.Lock()
	n, exists := s.nodes[id]
	if exists {
		n.alive.Store(false)
		delete(s.nodes, id)
	}
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

func Serve(cfg ServerConfig) {
	lis, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := NewServer(cfg)

	grpcServer := grpc.NewServer()
	pb.RegisterNetworkControllerServer(grpcServer, srv)
	log.Println("Controller listening on :50051...")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}