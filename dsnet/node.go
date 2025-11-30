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
// It provides the necessary metadata for the dsnet package to route the message.
type BaseMessage struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // Must be set to the struct name for easy unmarshalling
}

// Event represents a message received by the Node. The Payload must be
// unmarshaled by the student's code according to the message Type.
type Event struct {
	From    string // Sender's Node ID
	To      string // Recipient's Node ID
	Type    string // Protocol Type (e.g., "RequestVote", "Commit")
	Payload []byte // Raw JSON payload of the message
}

// Node is the student's entry point to the distributed network.
type Node struct {
	ID string

	// Public channel where the student's algorithm receives incoming messages.
	// The student's main loop listens on this channel.
	Inbound chan Event

	// Internal gRPC components
	stream pb.NetworkController_StreamClient
	conn   *grpc.ClientConn
	wg     sync.WaitGroup
}

// NewNode initializes the network connection, performs the handshake, and
// starts the listener goroutine.
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
		ID:      id,
		conn:    conn,
		stream:  stream,
		Inbound: make(chan Event, 100),
	}

	// Send Handshake
	if err := stream.Send(&pb.Envelope{From: id, To: "CTRL", Type: "HANDSHAKE"}); err != nil {
		n.Close()
		return nil, fmt.Errorf("handshake failed: %w", err)
	}
	log.Printf("[dsnet] Node %s connected to controller.", id)
	n.listenForSignal()

	// Start the background loop to receive messages from the controller
	n.wg.Add(1)
	go n.runRecvLoop()

	return n, nil
}

// Close gracefully shuts down the network connection and loops.
func (n *Node) Close() {
	// Closing the stream and connection will cause runRecvLoop to exit
	n.stream.CloseSend()
	n.conn.Close()
	n.wg.Wait()
	close(n.Inbound)
}

// Send wraps the student's Go struct into an Envelope and sends it to the destination.
// The msg argument MUST embed dsnet.BaseMessage.
func (n *Node) Send(ctx context.Context, dest string, msg interface{}) error {
	// 1. Marshal the struct into a raw JSON payload
	payloadBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("send error: failed to marshal struct: %w", err)
	}

	// 2. Extract Type field from the payload
	var base BaseMessage
	if err := json.Unmarshal(payloadBytes, &base); err != nil {
		return fmt.Errorf("send error: message must be valid JSON and embed BaseMessage: %w", err)
	}

	// 3. Create the final Envelope
	envelope := &pb.Envelope{
		From:    n.ID,
		To:      dest,
		Type:    base.Type,
		Payload: string(payloadBytes),
	}

	// 4. Send on the gRPC stream
	if err := n.stream.Send(envelope); err != nil {
		return fmt.Errorf("send error: gRPC stream failed: %w", err)
	}

	log.Printf("[dsnet] Sent %s to %s", base.Type, dest)
	return nil
}

// runRecvLoop continuously receives Envelopes and pushes them onto the public Inbound channel.
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

		// Push the raw payload onto the student's public inbound channel
		select {
		case n.Inbound <- Event{From: envelope.From, To: envelope.To, Type: envelope.Type, Payload: []byte(envelope.Payload)}:
			log.Printf("[dsnet] Received %s from %s", envelope.Type, envelope.From)
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
		log.Printf("[dsnet] Shutdown signal received for node %s", n.ID)
		n.Close()
		os.Exit(0)
	}()
}
