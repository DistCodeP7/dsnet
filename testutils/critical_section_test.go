package testutils

import (
	"sync"
	"testing"
	"time"
)

func TestCriticalSection(t *testing.T) {
	cs := &CriticalSection{}
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		cs.Work("A", 500*time.Millisecond, nil)
	}()
	go func() {
		defer wg.Done()
		cs.Work("B", 500*time.Millisecond, nil)
	}()
	go func() {
		defer wg.Done()
		cs.Work("C", 500*time.Millisecond, nil)
	}()

	wg.Wait()
	t.Logf("Critical section entered %d times", cs.Value())
}
