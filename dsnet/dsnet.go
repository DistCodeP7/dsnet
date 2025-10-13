package dsnet

import (
	"context"
	"io"
	"log"
	"sync"

	pb "gotest/dsnet/gotest/dsnet/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type DSNet struct {
	NodeID       string
	stream       pb.NetworkController_ControlStreamClient
	Inbox        chan *pb.Envelope
	RegisteredCh chan struct{}
	closeCh      chan struct{}
	closeOnce    sync.Once
}

// ConnectWithContext connects a node to the controller with a context.
func ConnectWithContext(ctx context.Context, controllerAddr, nodeID string) (*DSNet, error) {
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
	}

	// Register node with controller
	if err := stream.Send(&pb.ShimToCtrl{
		Payload: &pb.ShimToCtrl_Register{
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

// Broadcast sends a message to all nodes.
func (d *DSNet) Broadcast(msg string) error {
	env := &pb.Envelope{
		From:    d.NodeID,
		Payload: msg,
		Type:    pb.MessageType_BROADCAST,
	}
	return d.stream.Send(&pb.ShimToCtrl{
		Payload: &pb.ShimToCtrl_Outbound{Outbound: env},
	})
}

// Send sends a direct message to a single node.
func (d *DSNet) Send(to, msg string) error {
	env := &pb.Envelope{
		From:    d.NodeID,
		To:      to,
		Payload: msg,
		Type:    pb.MessageType_DIRECT,
	}
	return d.stream.Send(&pb.ShimToCtrl{
		Payload: &pb.ShimToCtrl_Outbound{Outbound: env},
	})
}

// Publish sends a message to a group.
func (d *DSNet) Publish(group, msg string) error {
	env := &pb.Envelope{
		From:    d.NodeID,
		Group:   group,
		Payload: msg,
		Type:    pb.MessageType_GROUP,
	}
	return d.stream.Send(&pb.ShimToCtrl{
		Payload: &pb.ShimToCtrl_Outbound{Outbound: env},
	})
}

// Subscribe subscribes the node to a group.
func (d *DSNet) Subscribe(group string) error {
	return d.stream.Send(&pb.ShimToCtrl{
		Payload: &pb.ShimToCtrl_Subscribe{
			Subscribe: &pb.SubscribeReq{
				NodeId: d.NodeID,
				Group:  group,
			},
		},
	})
}

// Unsubscribe unsubscribes the node from a group.
func (d *DSNet) Unsubscribe(group string) error {
	return d.stream.Send(&pb.ShimToCtrl{
		Payload: &pb.ShimToCtrl_Unsubscribe{
			Unsubscribe: &pb.UnsubscribeReq{
				NodeId: d.NodeID,
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
