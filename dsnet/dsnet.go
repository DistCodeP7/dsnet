package dsnet

import (
	"context"
	"fmt"
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
	nodeId       string
	stream       pb.NetworkController_ControlStreamClient
	Inbox        chan *pb.Envelope
	registeredCh chan struct{}
	closeCh      chan struct{}
	vclock       *pb.VClock
	seq          uint64
	closeOnce    sync.Once

	mu       sync.RWMutex
	handlers map[string]func(ctx context.Context, from string, env *pb.Envelope) error
}

func (d *DSNet) GetNodeID() string {
	return d.nodeId
}

func Connect(controllerAddr, nodeID string) (*DSNet, error) {
	return ConnectWithContext(context.Background(), controllerAddr, nodeID)
}

// ConnectWithContext connects a node to the controller with a context.
func ConnectWithContext(ctx context.Context, controllerAddr, nodeId string) (*DSNet, error) {
	clientConn, err := grpc.NewClient(controllerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	client := pb.NewNetworkControllerClient(clientConn)
	stream, err := client.ControlStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream: %w", err)
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
		seq:      0,
		handlers: make(map[string]func(ctx context.Context, from string, env *pb.Envelope) error),
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

	// Block until a registered response has been recieved
	select {
	case <-ds.registeredCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return ds, nil
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
				env := payload.Forward
				d.dispatch(context.TODO(), env)

			default:
				log.Printf("[%s] unknown payload type received: %T", d.nodeId, payload)
			}
		}
	}
}
