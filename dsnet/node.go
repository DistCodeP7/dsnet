package dsnet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	pb "github.com/distcodep7/dsnet/proto"
	"github.com/distcodep7/dsnet/testing"
	"github.com/google/uuid"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type BaseMessage struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

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

	logFile *os.File
	logMu   sync.Mutex
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

	f, err := os.OpenFile("trace_log.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open execution log file: %w", err)
	}

	n := &Node{
		ID:          id,
		conn:        conn,
		stream:      stream,
		Inbound:     make(chan Event, 100),
		vectorClock: make(map[string]uint64),
		logFile:     f,
	}

	n.vectorClock[id] = 0

	if err := stream.Send(&pb.Envelope{
		Id:   uuid.NewString(),
		From: id,
		To:   "CTRL",
		Type: "HANDSHAKE",
	}); err != nil {
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
	n.logFile.Close()
}

func (n *Node) Send(ctx context.Context, dest string, msg interface{}) error {
	payloadBytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	var base BaseMessage
	if err := json.Unmarshal(payloadBytes, &base); err != nil {
		return fmt.Errorf("message must be valid JSON: %w", err)
	}

	n.vcMu.Lock()
	n.vectorClock[n.ID]++

	currentVecProto := n.toProtoVector()

	currentVecMap := make(map[string]uint64)
	maps.Copy(currentVecMap, n.vectorClock)
	n.vcMu.Unlock()

	msgID := uuid.NewString()

	envelope := &pb.Envelope{
		Id:      msgID,
		From:    n.ID,
		To:      dest,
		Type:    base.Type,
		Payload: string(payloadBytes),
		Vector:  currentVecProto,
	}

	n.logEvent(testing.EvtTypeSend, msgID, base.Type, n.ID, dest, currentVecMap, payloadBytes)

	if err := n.stream.Send(envelope); err != nil {
		return fmt.Errorf("gRPC send failed: %w", err)
	}

	return nil
}

func (n *Node) runRecvLoop() {
	defer n.wg.Done()

	for {
		envelope, err := n.stream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Printf("Stream error: %v", err)
			return
		}

		if envelope.Type == "STOP" {
			n.Close()
			os.Exit(0)
		}

		n.vcMu.Lock()
		incomingVec := n.fromProtoVector(envelope.Vector)
		for id, val := range incomingVec {
			if val > n.vectorClock[id] {
				n.vectorClock[id] = val
			}
		}
		n.vectorClock[n.ID]++

		newClockState := make(map[string]uint64)
		maps.Copy(newClockState, n.vectorClock)
		n.vcMu.Unlock()

		n.logEvent(testing.EvtTypeRecv, envelope.Id, envelope.Type, envelope.From, envelope.To, newClockState, []byte(envelope.Payload))

		select {
		case n.Inbound <- Event{
			From:        envelope.From,
			To:          envelope.To,
			Type:        envelope.Type,
			Payload:     []byte(envelope.Payload),
			VectorClock: incomingVec,
		}:
		default:
			log.Printf("WARNING: Inbound full. Dropped msg from %s", envelope.From)
		}
	}
}

func (n *Node) logEvent(evtType testing.EvtType, msgID, msgType, from, to string, vc map[string]uint64, payload []byte) {
	entry := testing.TraceEvent{
		ID:          uuid.NewString(),
		MessageID:   msgID,
		Timestamp:   time.Now().UnixNano(),
		EvtType:     evtType,
		MsgType:     msgType,
		From:        from,
		To:          to,
		VectorClock: vc,
		Payload:     json.RawMessage(payload),
	}

	n.logMu.Lock()
	defer n.logMu.Unlock()

	encoder := json.NewEncoder(n.logFile)
	_ = encoder.Encode(entry)
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
