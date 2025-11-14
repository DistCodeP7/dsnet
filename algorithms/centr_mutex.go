package algorithms

import (
	"sync"

	"github.com/DistCodeP7/dsnet/dsnet"
	pb "github.com/DistCodeP7/dsnet/proto"
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

func (h *MutexHandler) OnEvent(env *pb.Envelope) []*pb.Envelope {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch env.Type {
	case pb.MessageType_REQUEST_TOKEN:
		if h.hasToken {
			h.hasToken = false

			return []*pb.Envelope{
				{
					From:    h.NodeID,
					To:      env.From,
					Payload: "TOKEN",
					Type:    pb.MessageType_TOKEN,
				},
			}
		} else {
			h.waitQueue = append(h.waitQueue, env.From)
		}

	case pb.MessageType_TOKEN:
		h.hasToken = true

		if h.waiting {
			h.waiting = false
			h.csEntryCh <- struct{}{}
		}
	}

	return nil
}

func (h *MutexHandler) NumWaiting() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.waitQueue)
}

func (h *MutexHandler) NextWaiting() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.waitQueue) == 0 {
		return ""
	}

	next := h.waitQueue[0]
	h.waitQueue = h.waitQueue[1:]
	return next
}
