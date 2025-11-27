package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	cmdPath     string
	mu          sync.Mutex
	currentProc *exec.Cmd
)

func Start(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	if currentProc != nil {
		fmt.Fprintln(w, "Process already running")
		return
	}

	if err := startProcess(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start process: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintln(w, "Process started")
}

func Reset(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	if currentProc != nil {
		stopProcess()
	}

	if err := startProcess(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to restart process: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintln(w, "Process reset")
}

func Stop(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	if currentProc == nil {
		fmt.Fprintln(w, "No process running")
		return
	}

	stopProcess()
	fmt.Fprintln(w, "Process stopped")
}

func startProcess() error {
	currentProc = exec.Command(cmdPath)
	currentProc.Stdout = os.Stdout
	currentProc.Stderr = os.Stderr

	if err := currentProc.Start(); err != nil {
		currentProc = nil
		return err
	}

	go func() {
		// Wait for the process to finish
		err := currentProc.Wait()
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			fmt.Printf("Process exited with error: %v\n", err)
		} else {
			fmt.Println("Process exited successfully")
		}
		currentProc = nil
	}()

	return nil
}

func stopProcess() {
	if currentProc == nil {
		return
	}

	// Try graceful termination first
	currentProc.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		currentProc.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		fmt.Println("Process did not exit, killing")
		currentProc.Process.Kill()
	}

	currentProc = nil
}

func main() {
	flag.StringVar(&cmdPath, "cmd", "", "Path to the student executable")
	flag.Parse()

	if cmdPath == "" {
		fmt.Println("You must provide -cmd")
		os.Exit(1)
	}

	fmt.Println("Starting wrapper for:", cmdPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &http.Server{Addr: ":8090"}

	http.HandleFunc("/start", Start)
	http.HandleFunc("/reset", Reset)
	http.HandleFunc("/stop", Stop)

	go func() {
		fmt.Println("Server running on :8090")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Println("Server error:", err)
			cancel()
		}
	}()

	// Listen for signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigCh:
		fmt.Println("Received termination signal, shutting down...")
	case <-ctx.Done():
		fmt.Println("Context canceled, shutting down...")
	}

	// Shutdown HTTP server gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Println("Error shutting down server:", err)
	}

	// Stop the running process if any
	mu.Lock()
	stopProcess()
	mu.Unlock()

	fmt.Println("Wrapper exited")
}
