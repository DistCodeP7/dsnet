package algorithms

import (
	"sync"

	"github.com/distcode/dsnet/dsnet"
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

// RequestToken sends a request to the server
func (c *MutexClient) RequestToken(d *dsnet.DSNet, serverID string) {
	c.mu.Lock()
	c.waiting = true
	c.mu.Unlock()

	_ = d.Send(serverID, "REQUEST_TOKEN", pb.MessageType_REQUEST_TOKEN)
}

// ReleaseToken sends the token back to the server after critical section
func (c *MutexClient) ReleaseToken(d *dsnet.DSNet, serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.hasToken {
		return
	}
	c.hasToken = false
	_ = d.Send(serverID, "RELEASE_TOKEN", pb.MessageType_RELEASE_TOKEN)
}

// OnEvent handles incoming messages from the server
func (c *MutexClient) OnEvent(env *pb.Envelope) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if env.Type == pb.MessageType_TOKEN {
		c.hasToken = true
		if c.waiting {
			c.waiting = false
			c.csEntryCh <- struct{}{}
		}
	}
}

// WaitChan returns a channel for entering the critical section
func (c *MutexClient) WaitChan() <-chan struct{} {
	return c.csEntryCh
}
