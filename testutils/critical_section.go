package testutils

import (
	"fmt"
	"sync"
	"time"
)

type CriticalSection struct {
	mu    sync.Mutex
	value int
}

func (cs *CriticalSection) Work(nodeID string, duration time.Duration, f func()) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	fmt.Printf("[%s] ENTER CS\n", nodeID)
	if f != nil {
		f()
	}
	time.Sleep(duration)
	cs.value++
	fmt.Printf("[%s] EXIT CS\n", nodeID)
}

func (cs *CriticalSection) Value() int {
	return cs.value
}
