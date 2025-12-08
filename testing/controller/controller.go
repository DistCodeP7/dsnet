package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/distcodep7/dsnet/proto"
	"github.com/distcodep7/dsnet/testing"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

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
	DropProb        float64
	DupeProb        float64
	AsyncDuplicate  bool
	ReorderProb     float64
	ReorderMinDelay int
	ReorderMaxDelay int
}

type Server struct {
	pb.UnimplementedNetworkControllerServer

	mu      sync.Mutex
	nodes   map[string]*Node
	senders map[string]sender
	blocked map[string]map[string]bool
	rng     *rand.Rand
	rngMu   sync.Mutex

	testConfig TestConfig

	// File for structural logging
	logFile *os.File
	logMu   sync.Mutex
}

func NewTestConfig(dropp, reordp, dupep float64, asyncDup bool, reordMin, reordMax int) TestConfig {
	return TestConfig{
		DropProb:        dropp,
		ReorderProb:     reordp,
		DupeProb:        dupep,
		AsyncDuplicate:  asyncDup,
		ReorderMinDelay: reordMin,
		ReorderMaxDelay: reordMax,
	}
}

func NewServer(cfg TestConfig) *Server {
	f, err := os.OpenFile("trace_log.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open execution log file: %v", err)
	}

	return &Server{
		nodes:      make(map[string]*Node),
		senders:    make(map[string]sender),
		blocked:    make(map[string]map[string]bool),
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		testConfig: cfg,
		logFile:    f,
	}
}

func (s *Server) logDrop(env *pb.Envelope) {
	s.logMu.Lock()
	defer s.logMu.Unlock()

	vcMap := make(map[string]uint64)
	for _, entry := range env.Vector {
		vcMap[entry.Node] = entry.Counter
	}

	entry := testing.TraceEvent{
		ID:          uuid.NewString(),
		MessageID:   env.Id,
		Timestamp:   time.Now().UnixNano(),
		EvtType:     testing.EvtTypeDrop,
		MsgType:     env.Type,
		From:        env.From,
		To:          env.To,
		VectorClock: vcMap,
		Payload:     json.RawMessage(env.Payload),
	}

	encoder := json.NewEncoder(s.logFile)
	if err := encoder.Encode(entry); err != nil {
		log.Printf("[ERR] Failed to write to log file: %v", err)
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
	s.senders[nodeID] = n
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

		if msg.To == "CTRL" {
			continue
		}
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

	// Partition Check
	if blockedTargets, exists := s.blocked[msg.From]; exists {
		if blockedTargets[msg.To] {
			log.Printf("[PARTITION] Dropped: %s -> %s", msg.From, msg.To)
			s.mu.Unlock()
			s.logDrop(msg)
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
		delete(s.senders, id)
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

func Serve(cfg TestConfig) {
	lis, err := net.Listen("tcp", ":50051")
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
