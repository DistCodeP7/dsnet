package algorithms

import (
	"sync"

	pb "github.com/distcode/dsnet/proto"
)

// MutexClient is a token-based client for requesting/releasing a critical section.
type MutexClient struct {
	mu        sync.Mutex
	NodeID    string
	hasToken  bool
	waiting   bool
	csEntryCh chan struct{}
}

// NewMutexClient creates a new MutexClient instance.
func NewMutexClient(nodeID string) *MutexClient {
	return &MutexClient{
		NodeID:    nodeID,
		csEntryCh: make(chan struct{}, 1),
	}
}

// RequestToken prepares a request envelope to the server.
// Users should implement logic for sending or processing the envelope.
func (c *MutexClient) RequestToken(serverID string) *pb.Envelope {
	// TODO: Implement logic to request token
	return &pb.Envelope{
		From:    c.NodeID,
		To:      serverID,
		Payload: "REQUEST_TOKEN",
		Type:    pb.MessageType_REQUEST_TOKEN,
	}
}

// ReleaseToken prepares a release envelope after leaving the critical section.
// Users should implement logic to release the token.
func (c *MutexClient) ReleaseToken(serverID string) *pb.Envelope {
	// TODO: Implement logic to release token
	return &pb.Envelope{
		From:    c.NodeID,
		To:      serverID,
		Payload: "RELEASE_TOKEN",
		Type:    pb.MessageType_RELEASE_TOKEN,
	}
}

// OnEvent handles incoming messages from the server.
// Users should implement their own event handling logic.
func (c *MutexClient) OnEvent(env *pb.Envelope) []*pb.Envelope {
	// TODO: Implement custom event handling logic
	return nil
}

// WaitChan returns a channel to signal entry into the critical section.
// Users may use or modify this as needed.
func (c *MutexClient) WaitChan() <-chan struct{} {
	return c.csEntryCh
}
