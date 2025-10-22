package dsnet

import (
	"context"
	"io"
	"log"
	"sync"

	pb "github.com/distcode/dsnet/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

/*
* DSNet provides a simple interface for nodes to communicate via a central controller.
* Nodes can send direct messages, broadcast messages, and publish to groups.
* Each node maintains an inbox channel for incoming messages.
 */
type DSNet struct {
	nodeId       string
	stream       pb.NetworkController_ControlStreamClient
	Inbox        chan *pb.Envelope
	registeredCh chan struct{}
	closeCh      chan struct{}
	vclock       *pb.VClock
	seq          uint64
	closeOnce    sync.Once
}

func (d *DSNet) GetNodeID() string {
	return d.nodeId
}

// ConnectWithContext connects a node to the controller with a context.
func ConnectWithContext(ctx context.Context, controllerAddr, nodeId string) (*DSNet, error) {
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
		nodeId:       nodeId,
		stream:       stream,
		Inbox:        make(chan *pb.Envelope, 100),
		registeredCh: make(chan struct{}),
		closeCh:      make(chan struct{}),
		vclock: &pb.VClock{
			Vclock: make(map[string]uint64),
		},
		seq: 0,
	}

	// Register node with controller
	if err := stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Register{
			Register: &pb.RegisterReq{NodeId: nodeId},
		},
	}); err != nil {
		return nil, err
	}

	go ds.listen()

	select {
	case <-ds.registeredCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return ds, nil
}

func Connect(controllerAddr, nodeID string) (*DSNet, error) {
	return ConnectWithContext(context.Background(), controllerAddr, nodeID)
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
				log.Printf("[%s] stream closed by server", d.nodeId)
				return
			}
			if err != nil {
				log.Printf("[%s] receive error: %v", d.nodeId, err)
				continue
			}

			switch payload := in.Payload.(type) {
			case *pb.ControllerToClient_Register:
				select {
				case d.registeredCh <- struct{}{}:
				default:
				}

			case *pb.ControllerToClient_Forward:
				d.Inbox <- payload.Forward

			default:
				log.Printf("[%s] unknown payload type received: %T", d.nodeId, payload)
			}
		}
	}
}

// helper to wrap a generic payload in structpb
func toStructPB(data map[string]interface{}) (*structpb.Struct, error) {
	return structpb.NewStruct(data)
}

// Broadcast sends a message to all nodes.
func (d *DSNet) Broadcast(payload map[string]interface{}, operation string) error {
	structPayload, err := toStructPB(payload)
	if err != nil {
		return err
	}
	env := &pb.Envelope{
		From:         d.nodeId,
		Payload:      structPayload,
		DeliveryType: pb.DeliveryType_BROADCAST,
		Operation:    operation,
		Seq:          d.seq,
	}
	d.seq++
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Send{Send: env},
	})
}

// Send sends a direct message to a single node.
func (d *DSNet) Send(to string, payload map[string]interface{}, operation string) error {
	structPayload, err := toStructPB(payload)
	if err != nil {
		return err
	}
	env := &pb.Envelope{
		From:         d.nodeId,
		To:           to,
		Payload:      structPayload,
		DeliveryType: pb.DeliveryType_DIRECT,
		Operation:    operation,
		Seq:          d.seq,
	}
	d.seq++
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Send{Send: env},
	})
}

// Publish sends a message to a group.
func (d *DSNet) Publish(group string, payload map[string]interface{}, operation string) error {
	structPayload, err := toStructPB(payload)
	if err != nil {
		return err
	}
	env := &pb.Envelope{
		From:         d.nodeId,
		Group:        group,
		Payload:      structPayload,
		DeliveryType: pb.DeliveryType_GROUP,
		Operation:    operation,
		Seq:          d.seq,
	}
	d.seq++
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Send{
			Send: env,
		},
	})
}

// Subscribe subscribes the node to a group.
func (d *DSNet) Subscribe(group string) error {
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Subscribe{
			Subscribe: &pb.SubscribeReq{
				NodeId: d.nodeId,
				Group:  group,
			},
		},
	})
}

// Unsubscribe unsubscribes the node from a group.
func (d *DSNet) Unsubscribe(group string) error {
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Unsubscribe{
			Unsubscribe: &pb.UnsubscribeReq{
				NodeId: d.nodeId,
				Group:  group,
			},
		},
	})
}

// Recv reads a message from the Inbox channel.
func (d *DSNet) Recv() (*pb.Envelope, bool) {
	env, ok := <-d.Inbox
	return env, ok
}
