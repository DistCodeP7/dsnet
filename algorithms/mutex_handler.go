package algorithms

import (
	"sync"

	"github.com/distcode/dsnet/dsnet"
	pb "github.com/distcode/dsnet/proto"
)

// An implementation of a token-based centralized mutex algorithm
type MutexHandler struct {
	mu        sync.Mutex
	NodeID    string
	hasToken  bool
	waiting   bool
	requestCh chan struct{}
	csEntryCh chan struct{}
	waitQueue []string
}

func NewMutexHandler(nodeID string, initialToken bool) *MutexHandler {
	return &MutexHandler{
		NodeID:    nodeID,
		hasToken:  initialToken,
		requestCh: make(chan struct{}, 1),
		csEntryCh: make(chan struct{}, 1),
	}
}

func (h *MutexHandler) RequestToken(d *dsnet.DSNet, tokenHolder string) {
	h.mu.Lock()
	if h.hasToken {
		h.hasToken = false
		h.mu.Unlock()
		h.csEntryCh <- struct{}{}
		return
	}
	h.waiting = true
	h.mu.Unlock()

	// Send a REQUEST_TOKEN to the current token holder. Use the correct
	// message type (REQUEST_TOKEN) so the recipient handles it as a request.
	_ = d.Send(tokenHolder, "REQUEST_TOKEN", pb.MessageType_REQUEST_TOKEN)
}

func (h *MutexHandler) ReleaseToken(d *dsnet.DSNet, serverId string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.hasToken {
		return nil
	}

	h.hasToken = false
	return d.Send(serverId, "RELEASE_TOKEN", pb.MessageType_RELEASE_TOKEN)
}

func (h *MutexHandler) OnEvent(env *pb.Envelope) []*pb.Envelope {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch env.Type {
	case pb.MessageType_TOKEN:
		h.hasToken = true
		if h.waiting {
			h.waiting = false
			h.csEntryCh <- struct{}{}
		}
	}

	return nil
}

func (h *MutexHandler) WaitChan() <-chan struct{} {
	return h.csEntryCh
}