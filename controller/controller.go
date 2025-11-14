package controller

import (
	"io"
	"sync"

	pb "github.com/DistcodeP7/dsnet/proto"
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

func (c *Controller) forward(env *pb.Envelope) {
	c.mu.Lock()
	dest, ok := c.nodes[env.To]
	c.mu.Unlock()
	if !ok {
		c.log.Printf("Unknown destination: %s", env.To)
		return
	}

	go func() {
		if err := dest.Send(&pb.ControllerToClient{Inbound: env}); err != nil {
			c.log.Printf("Failed to send to %s: %v", env.To, err)
		} else {
			c.log.Printf("Forwarded %s -> %s: %s", env.From, env.To, env.Payload)
		}
	}()
}