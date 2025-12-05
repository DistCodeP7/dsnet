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
	cancelFunc  context.CancelFunc
	srv         *http.Server
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

func Shutdown(w http.ResponseWriter, _ *http.Request) {
	// Fully terminate both student and wrapper
	mu.Lock()
	stopProcess()
	mu.Unlock()

	fmt.Fprintln(w, "Wrapper shutting down...")
	cancelFunc() // triggers shutdown in main()
}

func Ready(w http.ResponseWriter, _ *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	if currentProc != nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Ready")
	} else {
		http.Error(w, "Not ready", http.StatusServiceUnavailable)
	}
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
	cancelFunc = cancel

	srv = &http.Server{Addr: ":8090"}

	http.HandleFunc("/ready", Ready)
	http.HandleFunc("/start", Start)
	http.HandleFunc("/reset", Reset)
	http.HandleFunc("/stop", Stop)
	http.HandleFunc("/shutdown", Shutdown)

	go func() {
		fmt.Println("Server running on :8090")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Println("Server error:", err)
			cancel()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigCh:
		fmt.Println("Received termination signal, shutting down...")
	case <-ctx.Done():
		fmt.Println("Shutdown requested...")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)

	mu.Lock()
	stopProcess()
	mu.Unlock()

	fmt.Println("Wrapper exited")
}
