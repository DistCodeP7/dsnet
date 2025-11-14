package controller

import (
	"io"
	"sync"

	pb "github.com/distcodep7/dsnet/proto"
)

type ControllerProps struct {
	Logger Logger
}

/*
* Controller handles client connections and manages message routing.
* Will function as message broker for DSNet nodes.
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
		if err == io.EOF {
			return nil
		}
		if err != nil {
			c.log.Printf("stream recv error: %v", err)
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

		case *pb.ClientToController_Outbound:
			c.forward(payload.Outbound)
		}
	}
}

func (c *Controller) sendRegisteredResponse(nodeID string) {
	c.mu.Lock()
	stream, ok := c.nodes[nodeID]
	c.mu.Unlock()
	if !ok {
		c.log.Printf("sendRegisteredResponse: node %s not found", nodeID)
		return
	}

	resp := &pb.Envelope{
		From:    "controller",
		Payload: "registered",
		Type:    pb.MessageType_REGISTERED,
	}

	if err := stream.Send(&pb.ControllerToClient{Inbound: resp}); err != nil {
		c.log.Printf("Failed to send REGISTERED to %s: %v", nodeID, err)
	}
}

func (c *Controller) addToGroup(nodeID, group string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.groups[group] == nil {
		c.groups[group] = make(map[string]struct{})
	}
	c.groups[group][nodeID] = struct{}{}
	c.log.Printf("Node %s subscribed to group %s", nodeID, group)
}

func (c *Controller) removeFromGroup(nodeID, group string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.groups[group]; ok {
		delete(c.groups[group], nodeID)
		c.log.Printf("Node %s unsubscribed from group %s", nodeID, group)
	}
}

func (c *Controller) forward(env *pb.Envelope) {
	switch env.Type {
	case pb.MessageType_BROADCAST:
		c.mu.Lock()
		defer c.mu.Unlock()

		for nodeID, stream := range c.nodes {
			if err := stream.Send(&pb.ControllerToClient{Inbound: env}); err != nil {
				c.log.Printf("Failed to broadcast to %s: %v", nodeID, err)
			}
		}
		c.log.Printf("Broadcasted from %s: %s", env.From, env.Payload)
	case pb.MessageType_GROUP:
		c.mu.Lock()
		members := c.groups[env.Group]
		c.mu.Unlock()

		for id := range members {
			if stream, ok := c.nodes[id]; ok {
				stream.Send(&pb.ControllerToClient{Inbound: env})
			}
		}

	default:
		c.mu.Lock()
		dest, ok := c.nodes[env.To]
		c.mu.Unlock()
		if !ok {
			c.log.Printf("Unknown destination: %s", env.To)
			return
		}
		if err := dest.Send(&pb.ControllerToClient{Inbound: env}); err != nil {
			c.log.Printf("Failed to send to %s: %v", env.To, err)
		} else {
			c.log.Printf("Forwarded %s -> %s: %s", env.From, env.To, env.Payload)
		}
	}
}
