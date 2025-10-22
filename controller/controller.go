package controller

import (
	"io"
	"sync"

	pb "github.com/distcode/dsnet/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ControllerProps struct {
	Logger Logger
}

/*
* Controller handles client connections and manages message routing.
* Functions as a message broker for DSNet nodes.
 */
type Controller struct {
	pb.UnimplementedNetworkControllerServer
	mu     sync.Mutex
	nodes  map[string]pb.NetworkController_ControlStreamServer
	groups map[string]map[string]struct{}
	log    Logger
}

func NewController(props ControllerProps) *Controller {
	if props.Logger == nil {
		props.Logger = &NoOpLogger{}
	}

	return &Controller{
		nodes:  make(map[string]pb.NetworkController_ControlStreamServer),
		groups: make(map[string]map[string]struct{}),
		log:    props.Logger,
	}
}

func (c *Controller) ControlStream(stream pb.NetworkController_ControlStreamServer) error {
	var nodeID string

	for {
		in, err := stream.Recv()

		if err != nil {
			if err == io.EOF {
				return nil
			}
			if grpcErr, ok := status.FromError(err); ok && grpcErr.Code() == codes.Canceled {
				c.mu.Lock()
				delete(c.nodes, nodeID)
				c.mu.Unlock()
				c.log.Printf("Node disconnected: %s", nodeID)
				return nil
			}
			c.log.Printf("stream recv non-expected error: %v", err)
			return err
		}

		switch payload := in.Payload.(type) {
		case *pb.ClientToController_Register:
			nodeID = payload.Register.NodeId
			c.mu.Lock()
			c.nodes[nodeID] = stream
			c.mu.Unlock()
			c.log.Printf("Registered node: %s", nodeID)
			c.sendRegisteredResponse(nodeID)

		case *pb.ClientToController_Subscribe:
			c.addToGroup(payload.Subscribe.NodeId, payload.Subscribe.Group)

		case *pb.ClientToController_Unsubscribe:
			c.removeFromGroup(payload.Unsubscribe.NodeId, payload.Unsubscribe.Group)

		case *pb.ClientToController_Send:
			c.forward(payload.Send)
		}
	}
}

func (c *Controller) sendRegisteredResponse(nodeId string) {
	c.mu.Lock()
	stream, ok := c.nodes[nodeId]
	c.mu.Unlock()

	if !ok {
		c.log.Printf("sendRegisteredResponse: node %s not found", nodeId)
		return
	}

	if err := stream.Send(&pb.ControllerToClient{
		Payload: &pb.ControllerToClient_Register{},
	}); err != nil {
		c.log.Printf("Failed to send REGISTERED to %s: %v", nodeId, err)
	}
}

func (c *Controller) addToGroup(nodeId, group string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.groups[group] == nil {
		c.groups[group] = make(map[string]struct{})
	}
	c.groups[group][nodeId] = struct{}{}
	c.log.Printf("Node %s subscribed to group %s", nodeId, group)
}

func (c *Controller) removeFromGroup(nodeId, group string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.groups[group]; ok {
		delete(c.groups[group], nodeId)
		c.log.Printf("Node %s unsubscribed from group %s", nodeId, group)
	}
}

func (c *Controller) forward(env *pb.Envelope) {
	switch env.DeliveryType {
	case pb.DeliveryType_BROADCAST:
		c.mu.Lock()
		defer c.mu.Unlock()

		for nodeID, stream := range c.nodes {
			if err := stream.Send(&pb.ControllerToClient{
				Payload: &pb.ControllerToClient_Forward{
					Forward: env,
				},
			}); err != nil {
				c.log.Printf("Failed to broadcast to %s: %v", nodeID, err)
			}
		}
		c.log.Printf("Broadcasted from %s: %v", env.From, env.Payload)

	case pb.DeliveryType_GROUP:
		c.mu.Lock()
		members := c.groups[env.Group]
		c.mu.Unlock()

		for id := range members {
			if stream, ok := c.nodes[id]; ok {
				err := stream.Send(&pb.ControllerToClient{
					Payload: &pb.ControllerToClient_Forward{
						Forward: env,
					},
				})
				if err != nil {
					c.log.Printf("Failed to send to %s in group %s: %v", id, env.Group, err)
				}
			}
		}

	default: // DIRECT
		c.mu.Lock()
		dest, ok := c.nodes[env.To]
		c.mu.Unlock()
		if !ok {
			c.log.Printf("Unknown destination: %s", env.To)
			return
		}
		if err := dest.Send(&pb.ControllerToClient{
			Payload: &pb.ControllerToClient_Forward{
				Forward: env,
			},
		}); err != nil {
			c.log.Printf("Failed to send to %s: %v", env.To, err)
		} else {
			c.log.Printf("Forwarded %s -> %s: %v", env.From, env.To, env.Payload)
		}
	}
}
