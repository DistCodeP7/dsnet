// Package wrapper provides a WrapperManager to control the lifecycle of binaries
// on multiple nodes via HTTP requests. It is intended for use in testing harnesses
// to start, stop, reset, and shutdown nodes, as well as to inject failures.
package wrapper

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

type Alias string

// WrapperManager manages the lifecycle of binaries on multiple nodes via HTTP requests.
// Intended for testing harness to control nodes and inject failures.
type WrapperManager struct {
	ManagedNodes []Alias
	Port         int
	Client       *http.Client
}

// NewWrapperManager creates a new WrapperManager for the specified nodes and port.
func NewWrapperManager(port int, nodes ...Alias) *WrapperManager {
	return &WrapperManager{
		ManagedNodes: nodes,
		Port:         port,
		Client:       &http.Client{},
	}
}

// Starts the binary on the specified node.
func (wm *WrapperManager) Start(ctx context.Context, node Alias) error {
	return wm.call(ctx, node, "start")
}

// Stops the binary on the specified node.
func (wm *WrapperManager) Stop(ctx context.Context, node Alias) error {
	return wm.call(ctx, node, "stop")
}

// Stops and then starts the binary on the specified node.
func (wm *WrapperManager) Reset(ctx context.Context, node Alias) error {
	return wm.call(ctx, node, "reset")
}

// Completely shuts down the wrapper and the binary on the specified node.
func (wm *WrapperManager) Shutdown(ctx context.Context, node Alias) error {
	return wm.call(ctx, node, "shutdown")
}

// Checks whether the binary on the specified node is ready.
// This assumes the wrapper exposes a /ready endpoint that returns 2xx when ready.
func (wm *WrapperManager) Ready(ctx context.Context, node Alias) error {
	return wm.call(ctx, node, "ready")
}

// Checks whether the binary on all managed nodes is ready.
// Returns a map from node alias to any error returned by the /ready endpoint.
func (wm *WrapperManager) ReadyAll(ctx context.Context) map[Alias]error {
	return wm.parallel(ctx, wm.ManagedNodes, wm.Ready)
}

// Starts the binary on all managed nodes.
func (wm *WrapperManager) StartAll(ctx context.Context) map[Alias]error {
	return wm.parallel(ctx, wm.ManagedNodes, wm.Start)
}

// Stops the binary on all managed nodes.
func (wm *WrapperManager) StopAll(ctx context.Context) map[Alias]error {
	return wm.parallel(ctx, wm.ManagedNodes, wm.Stop)
}

// Resets the binary on all managed nodes.
func (wm *WrapperManager) ResetAll(ctx context.Context) map[Alias]error {
	return wm.parallel(ctx, wm.ManagedNodes, wm.Reset)
}

// Shuts down the wrapper and binary on all managed nodes.
func (wm *WrapperManager) ShutdownAll(ctx context.Context) map[Alias]error {
	return wm.parallel(ctx, wm.ManagedNodes, wm.Shutdown)
}

// call performs the HTTP POST to the node action endpoint using the provided context.
func (wm *WrapperManager) call(ctx context.Context, node Alias, action string) error {
	url := fmt.Sprintf("http://%s:%d/%s", node, wm.Port, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := wm.Client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("request %s: status %d", url, resp.StatusCode)
	}
	return nil
}

// parallel runs the provided action for each node concurrently and collects errors.
// action should be a function taking (context.Context, Alias) error (like wm.Start).
func (wm *WrapperManager) parallel(ctx context.Context, nodes []Alias, action func(context.Context, Alias) error) map[Alias]error {
	var wg sync.WaitGroup
	errCh := make(chan struct {
		node Alias
		err  error
	}, len(nodes))

	for _, n := range nodes {
		node := n
		wg.Go(func() {
			err := action(ctx, node)
			errCh <- struct {
				node Alias
				err  error
			}{node: node, err: err}
		})
	}

	wg.Wait()
	close(errCh)

	results := make(map[Alias]error, len(nodes))
	for e := range errCh {
		results[e.node] = e.err
	}

	return results
}
