package dsnet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	pb "github.com/distcodep7/dsnet/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// BaseMessage must be embedded by every exercise-specific message struct.
type BaseMessage struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// Event represents a message received by the Node.
type Event struct {
	From        string
	To          string
	Type        string
	Payload     []byte
	VectorClock map[string]uint64
}

type Node struct {
	ID string

	Inbound chan Event

	stream pb.NetworkController_StreamClient
	conn   *grpc.ClientConn
	wg     sync.WaitGroup

	vcMu        sync.Mutex
	vectorClock map[string]uint64
}

func NewNode(id string, controllerAddr string) (*Node, error) {
	conn, err := grpc.NewClient(controllerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to controller at %s: %w", controllerAddr, err)
	}

	client := pb.NewNetworkControllerClient(conn)
	stream, err := client.Stream(context.Background())
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	n := &Node{
		ID:          id,
		conn:        conn,
		stream:      stream,
		Inbound:     make(chan Event, 100),
		vectorClock: make(map[string]uint64),
	}

	// Initialize own clock
	n.vectorClock[id] = 0

	// Send Handshake
	if err := stream.Send(&pb.Envelope{From: id, To: "CTRL", Type: "HANDSHAKE"}); err != nil {
		n.Close()
		return nil, fmt.Errorf("handshake failed: %w", err)
	}
	n.listenForSignal()

	n.wg.Add(1)
	go n.runRecvLoop()

	return n, nil
}

func (n *Node) Close() {
	n.stream.CloseSend()
	n.conn.Close()
	n.wg.Wait()
	close(n.Inbound)
}

func (n *Node) Send(ctx context.Context, dest string, msg interface{}) error {
	payloadBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("send error: failed to marshal struct: %w", err)
	}

	var base BaseMessage
	if err := json.Unmarshal(payloadBytes, &base); err != nil {
		return fmt.Errorf("send error: message must be valid JSON and embed BaseMessage: %w", err)
	}

	// --- VECTOR CLOCK UPDATE START ---
	n.vcMu.Lock()
	// 1. Increment own clock on send
	n.vectorClock[n.ID]++
	// 2. Create a copy to send
	currentVec := n.toProtoVector()
	n.vcMu.Unlock()
	// --- VECTOR CLOCK UPDATE END ---

	envelope := &pb.Envelope{
		From:    n.ID,
		To:      dest,
		Type:    base.Type,
		Payload: string(payloadBytes),
		// Send the vector slice
		Vector: currentVec,
	}

	if err := n.stream.Send(envelope); err != nil {
		return fmt.Errorf("send error: gRPC stream failed: %w", err)
	}

	log.Printf("[dsnet] Sent %s to %s (VC: %v)", base.Type, dest, currentVec)
	return nil
}

func (n *Node) runRecvLoop() {
	defer n.wg.Done()

	for {
		envelope, err := n.stream.Recv()
		if err == io.EOF {
			log.Printf("[dsnet] Controller closed stream.")
			return
		}
		if err != nil {
			log.Printf("[dsnet] Stream receive error: %v", err)
			return
		}

		if envelope.Type == "STOP" {
			log.Printf("[dsnet] STOP command received. Shutting down %s", n.ID)
			n.Close()
			os.Exit(0)
		}

		// --- VECTOR CLOCK MERGE START ---
		incomingVec := n.fromProtoVector(envelope.Vector)

		n.vcMu.Lock()
		// 1. Merge: VC[i] = max(VC[i], Incoming[i])
		for id, val := range incomingVec {
			if val > n.vectorClock[id] {
				n.vectorClock[id] = val
			}
		}
		// 2. Increment own clock on receive
		n.vectorClock[n.ID]++

		// Create a copy of state for the user to see
		stateCopy := make(map[string]uint64)
		for k, v := range n.vectorClock {
			stateCopy[k] = v
		}
		n.vcMu.Unlock()
		// --- VECTOR CLOCK MERGE END ---

		select {
		case n.Inbound <- Event{
			From:        envelope.From,
			To:          envelope.To,
			Type:        envelope.Type,
			Payload:     []byte(envelope.Payload),
			VectorClock: incomingVec, // Passing the sender's clock as context
		}:
			log.Printf("[dsnet] Received %s from %s. New State: %v", envelope.Type, envelope.From, stateCopy)
		default:
			log.Printf("[dsnet] WARNING: Inbound channel full. Dropping message from %s.", envelope.From)
		}
	}
}

func (n *Node) listenForSignal() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigs
		n.Close()
		os.Exit(0)
	}()
}

// Helpers for Vector Clock conversion
func (n *Node) toProtoVector() []*pb.VectorClockEntry {
	var entries []*pb.VectorClockEntry
	for id, c := range n.vectorClock {
		entries = append(entries, &pb.VectorClockEntry{Node: id, Counter: c})
	}
	return entries
}

func (n *Node) fromProtoVector(entries []*pb.VectorClockEntry) map[string]uint64 {
	vec := make(map[string]uint64)
	for _, e := range entries {
		vec[e.Node] = e.Counter
	}
	return vec
}
