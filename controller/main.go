package main

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
	"google.golang.org/protobuf/proto"
)

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

type Server struct {
	pb.UnimplementedNetworkControllerServer

	mu      	sync.Mutex
	nodes   	map[string]*Node
	blocked 	map[string]map[string]bool // blocked[A][B] = true means A cannot send to B
	rng     	*rand.Rand
	rngMu		sync.Mutex
	testConfig  TestConfig
}

// NewServer creates a Server with configurable RNG and probabilities.
// If rng is nil, a new rand.Rand will be created with a time-based seed.
func NewTestConfig(dropProb, dupeProb float64, asyncDupe bool, reorderProb float64, reorderMinDelay, reorderMaxDelay int) *TestConfig {
	return &TestConfig{
		DropProb:       dropProb,
		DupeProb:       dupeProb,
		AsyncDuplicate: asyncDupe,
		ReorderProb:  reorderProb,
		ReorderMinDelay: reorderMinDelay,
		ReorderMaxDelay: reorderMaxDelay,
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

// handleMessageEvents processes message events (drop, duplicate, reorder).
// It returns (true, nil) if the message delivery should be skipped (dropped or scheduled for later).
func (s *Server) handleMessageEvents(msg *pb.Envelope) (bool, error) {
	//DROP
	if s.probCheck(s.testConfig.DropProb) {
		if err := s.DropMessage(msg); err != nil {
			return false, fmt.Errorf("[DROP ERR] %v", err)
		}
		return true, nil
	}

	//DUPLICATE
	if s.probCheck(s.testConfig.DupeProb) {
		if err := s.DuplicateMessage(msg); err != nil {
			return false, fmt.Errorf("[DUPE ERR] %v", err)
		}
	}

	//REORDER
	if s.probCheck(s.testConfig.ReorderProb) {
		scheduled, err := s.ReorderMessage(msg)
		if err != nil {
			return false, fmt.Errorf("[REORD ERR] %v", err)
		}
		if scheduled {
			// message delivery delayed; do not send now
			return true, nil
		}
	}

	return false, nil
}

//ensureRNG lazily initializes the RNG if it is nil.
func (s *Server) ensureRNG() {
	s.rngMu.Lock()
	defer s.rngMu.Unlock()
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
}

//probCheck returns true with probability p.
func (s *Server) probCheck(p float64) bool {
	s.ensureRNG()
	s.rngMu.Lock()
	r := s.rng.Float64()
	s.rngMu.Unlock()
	return r < p
}

//randIntn returns a non-negative pseudo-random int in [0,n).
func (s *Server) randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	s.ensureRNG()
	s.rngMu.Lock()
	v := s.rng.Intn(n)
	s.rngMu.Unlock()
	return v
}

func (s *Server) DropMessage(msg *pb.Envelope) error {
	if msg == nil {
		return fmt.Errorf("[DROP ERR] Message is nil")
	}
	log.Printf("[DROP] Dropped: %s -> %s", msg.From, msg.To)
	return nil
}

func (s *Server) DuplicateMessage(msg *pb.Envelope) error {
	doAsync := true
	if !s.testConfig.AsyncDuplicate {
		doAsync = false
	}

	s.mu.Lock()
	target, ok := s.nodes[msg.To]
	s.mu.Unlock()
	if !ok {
		return nil
	}

	clone := proto.Clone(msg).(*pb.Envelope)

	if doAsync {
		go func(n *Node, m *pb.Envelope) {
			if err := n.send(m); err != nil {
				log.Printf("[DUPE ERR] %v", err)
			} else {
				log.Printf("[DUPE] Duplicated: %s -> %s", m.From, m.To)
			}
		}(target, clone)
		return nil
	}

	// Synchronous duplicate.
	if err := target.send(clone); err != nil {
		return err
	}
	log.Printf("[DUPE] Duplicated: %s -> %s", clone.From, clone.To)
	return nil
}

// delaySendWithDuration sends a scheduled message after a set amount of time.
func (s *Server) delaySendWithDuration(msg *pb.Envelope, d time.Duration) {
	s.mu.Lock()
	target, ok := s.nodes[msg.To]
	s.mu.Unlock()
	if !ok {
		log.Printf("[REORD ERR] Unknown destination for delayed send: %s", msg.To)
		return
	}

	clone := proto.Clone(msg).(*pb.Envelope)

	go func(n *Node, m *pb.Envelope, d time.Duration) {
		log.Printf("[REORD] Delaying: %s -> %s for %v", m.From, m.To, d)
		time.Sleep(d)
		if err := n.send(m); err != nil {
			log.Printf("[REORD ERR] failed send after delay: %v", err)
		} else {
			log.Printf("[REORD] Sent delayed: %s -> %s", m.From, m.To)
		}
	}(target, clone, d)
}

// ReorderMessage chooses a random delay between ReorderMinDelay and ReorderMaxDelay
// (seconds) and schedules the delayed send.
func (s *Server) ReorderMessage(msg *pb.Envelope) (bool, error) {
	min := s.testConfig.ReorderMinDelay
	max := s.testConfig.ReorderMaxDelay
	
	if min > max {
		return false, fmt.Errorf("ReorderMinDelay (%d) cannot be greater than ReorderMaxDelay (%d)", min, max)
	}

	var secs int
	if max == min {
		secs = min
	} else {
		secs = s.randIntn(max-min+1) + min
	}
	d := time.Duration(secs) * time.Second
	s.delaySendWithDuration(msg, d)
	return true, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	dropProb := 0.5
	dupeProb := 0.5
	asyncDupe := true
	reorderProb := 0.5
	reorderMinDelay := 1
	reorderMaxDelay := 10

	config := NewTestConfig(dropProb, dupeProb, asyncDupe, reorderProb, reorderMinDelay, reorderMaxDelay)

	srv := &Server {
		nodes:          make(map[string]*Node),
		blocked:        make(map[string]map[string]bool),
		rng:            rng,
		testConfig: 	*config,
	}

	grpcServer := grpc.NewServer()
	pb.RegisterNetworkControllerServer(grpcServer, srv)

	log.Println("Controller listening on :50051...")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}