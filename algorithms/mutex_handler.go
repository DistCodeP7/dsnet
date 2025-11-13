package algorithms

import (
	"sync"

	pb "github.com/distcode/dsnet/proto"
)

// MutexClient implements a token-based client for requesting/releasing critical section
type MutexClient struct {
	mu        sync.Mutex
	NodeID    string
	hasToken  bool
	waiting   bool
	csEntryCh chan struct{}
}

// NewMutexClient creates a new client
func NewMutexClient(nodeID string) *MutexClient {
	return &MutexClient{
		NodeID:    nodeID,
		csEntryCh: make(chan struct{}, 1),
	}
}

// RequestToken prepares a request envelope to be sent to the server.
func (c *MutexClient) RequestToken(serverID string) *pb.Envelope {
	c.mu.Lock()
	c.waiting = true
	c.mu.Unlock()

	return &pb.Envelope{
		From:    c.NodeID,
		To:      serverID,
		Payload: "REQUEST_TOKEN",
		Type:    pb.MessageType_REQUEST_TOKEN,
	}
}

// ReleaseToken prepares a release envelope to be sent to the server after
// leaving the critical section. It returns nil if the client does not hold
// the token.
func (c *MutexClient) ReleaseToken(serverID string) *pb.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.hasToken {
		return nil
	}
	c.hasToken = false
	return &pb.Envelope{
		From:    c.NodeID,
		To:      serverID,
		Payload: "RELEASE_TOKEN",
		Type:    pb.MessageType_RELEASE_TOKEN,
	}
}

// OnEvent handles incoming messages from the server. It now implements the
// project's NodeHandler contract (returning outgoing envelopes). It may
// produce outgoing messages in response to an event; currently the mutex
// handler only reacts locally (signals csEntry) and does not generate
// outgoing envelopes, so it returns nil.
func (c *MutexClient) OnEvent(env *pb.Envelope) []*pb.Envelope {
	c.mu.Lock()
	defer c.mu.Unlock()

	if env.Type == pb.MessageType_TOKEN {
		c.hasToken = true
		if c.waiting {
			c.waiting = false
			// non-blocking send in case nobody is waiting on the channel
			select {
			case c.csEntryCh <- struct{}{}:
			default:
			}
		}
	}
	return nil
}

// WaitChan returns a channel for entering the critical section
func (c *MutexClient) WaitChan() <-chan struct{} {
	return c.csEntryCh
}
