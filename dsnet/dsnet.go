package dsnet

import (
	"context"
	"io"
	"log"
	"sync"

	pb "github.com/distcode/dsnet/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

/*
* DSNet provides a simple interface for nodes to communicate via a central controller.
* Nodes can send direct messages, broadcast messages, and publish to groups.
* Each node maintains an inbox channel for incoming messages.
 */
type DSNet struct {
	NodeID       string
	stream       pb.NetworkController_ControlStreamClient
	Inbox        chan *pb.Envelope
	RegisteredCh chan struct{}
	closeCh      chan struct{}
	vclock       pb.VectorClock
	seq          uint64
	closeOnce    sync.Once
}

// ConnectWithContext connects a node to the controller with a context.
func ConnectWithContext(ctx context.Context, nodeID string) (*DSNet, error) {
	controllerAddr := "localhost:50051" // Default controller address; can be parameterized as needed.
	clientConn, err := grpc.NewClient(controllerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client := pb.NewNetworkControllerClient(clientConn)
	stream, err := client.ControlStream(ctx)
	if err != nil {
		return nil, err
	}

	ds := &DSNet{
		NodeID:       nodeID,
		stream:       stream,
		Inbox:        make(chan *pb.Envelope, 100),
		RegisteredCh: make(chan struct{}),
		closeCh:      make(chan struct{}),
		vclock: pb.VectorClock{
			Clock: make(map[string]uint64),
		},
		seq: 0,
	}

	// Register node with controller
	if err := stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Register{
			Register: &pb.RegisterReq{NodeId: nodeID},
		},
	}); err != nil {
		return nil, err
	}

	go ds.listen()

	select {
	case <-ds.RegisteredCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return ds, nil
}

func Connect(nodeID string) (*DSNet, error) {
	return ConnectWithContext(context.Background(), nodeID)
}

func (d *DSNet) Close() error {
	d.closeOnce.Do(func() {
		close(d.closeCh)
		close(d.Inbox)
	})
	return d.stream.CloseSend()
}

func (d *DSNet) listen() {
	for {
		select {
		case <-d.closeCh:
			return
		default:
			in, err := d.stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Printf("[%s] receive error: %v", d.NodeID, err)
				continue
			}
			if in.Inbound != nil && in.Inbound.Type == pb.MessageType_REGISTERED {
				select {
				case d.RegisteredCh <- struct{}{}:
				default:
				}
				continue
			}
			d.Inbox <- in.Inbound
		}
	}
}

// Recv reads a message from the Inbox channel.
func (d *DSNet) Recv() (*pb.Envelope, bool) {
	env, ok := <-d.Inbox
	return env, ok
}

func (d *DSNet) Send(to string, payload string, msgType pb.MessageType) error {
	env := &pb.Envelope{
		From:    d.NodeID,
		To:      to,
		Payload: payload,
		Type:    msgType,
	}
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Outbound{
			Outbound: env,
		},
	})
}

// TODO
func (d *DSNet) Broadcast(group string, payload string, msgType pb.MessageType) error {
	return d.Send("", payload, msgType)
}

type NodeHandler interface {
	OnEvent(env *pb.Envelope) []*pb.Envelope
}

func (d *DSNet) RunAlgo(handler NodeHandler) {
	for {
		select {
		case env, ok := <-d.Inbox:
			if !ok {
				return
			}
			outgoing := handler.OnEvent(env)
			for _, msg := range outgoing {
				_ = d.Send(msg.To, msg.Payload, msg.Type)
			}
		case <-d.closeCh:
			return
		}
	}
}
