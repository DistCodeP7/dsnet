package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type Path string

// E.g. the source code contained in a file
type SourceCode string

// Mmapping from file paths to their source code
type FileMap map[Path]SourceCode

// Eenvironment variable in the form "KEY=VALUE"
type EnvironmentVariable struct {
    Key   string
    Value string
}

type NodeSpec struct {
    Name  string
    Files FileMap
    Envs  []EnvironmentVariable
}

// --- Utility Functions ---

func PtrInt64(v int64) *int64 {
	return &v
}

func PtrBool(v bool) *bool {
	return &v
}

// --- Worker Implementation ---

// Worker is a wrapper around a single running Docker container responsible for code execution.
type Worker struct {
	containerID string
	dockerCli   *client.Client
	hostPath    string // Local path bound to the container
}

// WorkerInterface defines the methods available on a Worker instance.
type WorkerInterface interface {
	ID() string
	ConnectToNetwork(ctx context.Context, networkName, alias string) error
	DisconnectFromNetwork(ctx context.Context, networkName string) error
	Stop(ctx context.Context) error
	// ExecuteCode runs the entry code inside the container and streams output
	// It now accepts execution-specific environment variables.
	ExecuteCode(ctx context.Context, entryCommand string, execEnvs []EnvironmentVariable, stdoutCh, stderrCh chan string) error
}

var _ WorkerInterface = (*Worker)(nil)

// NewWorkerFromSpec initializes, configures, and starts a new Docker worker container
// based on the provided NodeSpec.
func NewWorkerFromSpec(ctx context.Context, cli *client.Client, workerImageName string, spec NodeSpec) (*Worker, error) {
	log.Printf("Initializing a new worker for spec %s (Image: %s)...", spec.Name, workerImageName)

	// Create a temporary directory on the host to mount into the container
	hostPath, err := os.MkdirTemp("", fmt.Sprintf("docker-worker-%s-*", spec.Name))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// 1. Write files from NodeSpec to the host path
	if err := writeSpecFiles(hostPath, spec.Files); err != nil {
		os.RemoveAll(hostPath)
		return nil, fmt.Errorf("failed to write spec files: %w", err)
	}

	// 2. Map environment variables for the *container itself*
	envVars := make([]string, 0, len(spec.Envs))
	for _, env := range spec.Envs {
		envVars = append(envVars, fmt.Sprintf("%s=%s", env.Key, env.Value))
	}

	// 3. Define Container Configuration
	containerConfig := &container.Config{
		Image: workerImageName,
		// Keep the container running in the background as a service
		Cmd:        []string{"sleep", "infinity"},
		Env:        envVars, // These are the base/default environment variables
		Tty:        false,
		WorkingDir: "/app",
		Hostname:   spec.Name, // Use the spec name as the internal hostname
	}

	// 4. Define Host Configuration (Resource limits and mounts)
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: hostPath,
				Target: "/tmp/", // Consistent mount point for files
			},
			{
				Type:   mount.TypeVolume,
				Source: "go-build-cache", // Assumes this volume exists or will be created
				Target: "/root/.cache/go-build",
			},
		},
		Resources: container.Resources{
			CPUShares:      512,
			NanoCPUs:       500_000_000,        // 0.5 CPU
			Memory:         512 * 1024 * 1024,  // 512MB
			MemorySwap:     1024 * 1024 * 1024, // 1GB
			PidsLimit:      PtrInt64(1024),     // max 1024 processes
			OomKillDisable: PtrBool(false),     // enable OOM killer
			Ulimits: []*container.Ulimit{
				{Name: "cpu", Soft: 30, Hard: 30},        // 30s CPU limit
				{Name: "nofile", Soft: 1024, Hard: 1024}, // max open files
			},
		},
	}

	// 5. Create the Container
	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, spec.Name)
	if err != nil {
		os.RemoveAll(hostPath)
		return nil, fmt.Errorf("failed to create container %s: %w", spec.Name, err)
	}

	// 6. Start the Container
	log.Printf("Starting container %s (%s)...", spec.Name, resp.ID[:12])
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}) // Attempt cleanup
		os.RemoveAll(hostPath)
		return nil, fmt.Errorf("failed to start container %s: %w", spec.Name, err)
	}

	worker := &Worker{
		containerID: resp.ID,
		dockerCli:   cli,
		hostPath:    hostPath,
	}

	log.Printf("Worker initialized with container %s", worker.containerID[:12])
	return worker, nil
}

// writeSpecFiles writes all files defined in the FileMap to the given host directory.
func writeSpecFiles(hostPath string, files FileMap) error {
	for filename, content := range files {
		// Prevent path traversal attacks or writing outside the intended directory
		cleanPath := filepath.Clean(string(filename))
		if strings.Contains(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			return fmt.Errorf("invalid filename path: %s", filename)
		}

		fullPath := filepath.Join(hostPath, cleanPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return err
		}

		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return err
		}
	}
	return nil
}

// ID returns the full Docker container ID.
func (w *Worker) ID() string {
	return w.containerID
}

// ConnectToNetwork connects the worker container to a specified Docker network.
func (w *Worker) ConnectToNetwork(ctx context.Context, networkName, alias string) error {
	return w.dockerCli.NetworkConnect(ctx, networkName, w.containerID, &network.EndpointSettings{
		Aliases: []string{alias},
	})
}

// DisconnectFromNetwork disconnects the worker container from a specified Docker network.
func (w *Worker) DisconnectFromNetwork(ctx context.Context, networkName string) error {
	return w.dockerCli.NetworkDisconnect(ctx, networkName, w.containerID, true)
}

// Stop gracefully stops, removes the container, and cleans up the host directory.
func (w *Worker) Stop(ctx context.Context) error {
	log.Printf("Stopping and removing container %s", w.containerID[:12])

	// Stop container
	if err := w.dockerCli.ContainerStop(ctx, w.containerID, container.StopOptions{}); err != nil {
		log.Printf("Warning: failed to gracefully stop container %s: %v", w.containerID[:12], err)
	}

	// Remove container
	if err := w.dockerCli.ContainerRemove(ctx, w.containerID, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	// Remove host directory
	if err := os.RemoveAll(w.hostPath); err != nil {
		return fmt.Errorf("failed to remove host path: %w", err)
	}

	return nil
}

// ExecuteCode runs an executable command inside the container and streams its output.
// It now accepts execution-specific environment variables (`execEnvs`) which override
// the container's default environment variables for this specific execution.
func (w *Worker) ExecuteCode(ctx context.Context, entryCommand string, execEnvs []EnvironmentVariable, stdoutCh, stderrCh chan string) error {
	// Convert EnvironmentVariable slice to string slice ("KEY=VALUE")
	execEnvStrings := make([]string, 0, len(execEnvs))
	for _, env := range execEnvs {
		execEnvStrings = append(execEnvStrings, fmt.Sprintf("%s=%s", env.Key, env.Value))
	}

	execConfig := container.ExecOptions{
		Cmd:          parseCommand(entryCommand),
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   "/app",
		Env:          execEnvStrings, // <-- NEW: Environment variables specific to this execution
	}

	execID, err := w.dockerCli.ContainerExecCreate(ctx, w.containerID, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec instance: %w", err)
	}

	hijackedResp, err := w.dockerCli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{
		Detach: false,
		Tty:    false,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to exec instance: %w", err)
	}
	defer hijackedResp.Close()

	// Use io.MultiWriter to simplify reading from the channels later
	stdoutWriter := newChannelWriter(stdoutCh)
	stderrWriter := newChannelWriter(stderrCh)
	done := make(chan error, 1)

	// Stream output
	go func() {
		// stdcopy.StdCopy demultiplexes the stream into stdout and stderr
		_, err := stdcopy.StdCopy(
			stdoutWriter,
			stderrWriter,
			hijackedResp.Reader,
		)
		stdoutWriter.Flush()
		stderrWriter.Flush()
		done <- err
	}()

	select {
	case <-ctx.Done():
		log.Printf("Job cancelled, stopping current execution in container %s", w.containerID)
		return ctx.Err()
	case err := <-done:
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to stream output: %w", err)
		}
	}

	// Inspect the exec result to get the exit code
	inspectResp, err := w.dockerCli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec instance: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return fmt.Errorf("execution finished with non-zero exit code: %d", inspectResp.ExitCode)
	}

	return nil
}

// parseCommand performs a simple split for command arguments.
func parseCommand(command string) []string {
	return strings.Fields(command)
}

// --- Channel Writer Implementation (Used for non-blocking stream handling) ---

type channelWriter struct {
	ch  chan string
	buf bytes.Buffer
}

func newChannelWriter(ch chan string) *channelWriter {
	return &channelWriter{ch: ch}
}

// Write implements the io.Writer interface. It buffers output and sends complete lines to the channel.
func (cw *channelWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i == -1 {
			// No newline found, buffer the rest
			cw.buf.Write(p)
			total += len(p)
			break
		}
		// Newline found, write up to (but not including) the newline
		cw.buf.Write(p[:i])
		cw.ch <- cw.buf.String()
		cw.buf.Reset()
		p = p[i+1:] // Move past the newline
		total += i + 1
	}
	return total, nil
}

// Flush sends any remaining content in the buffer to the channel.
func (cw *channelWriter) Flush() {
	if cw.buf.Len() > 0 {
		cw.ch <- cw.buf.String()
		cw.buf.Reset()
	}
}

// --- Main Execution Example ---

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if err := runExample(); err != nil {
		log.Fatalf("Example failed: %v", err)
	}
}

func runExample() error {
	// 1. Context timeout increased to 30 seconds for Go compilation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 2. Initialize Docker Client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer cli.Close()

	// 3. Define the static NodeSpec with base files and environment variables
	spec := NodeSpec{
		Name: "test-node-worker-go",
		Files: FileMap{
			"hello/main.go": `package main

import (
    "fmt"
    "os"
    "time"
)

func main() {
    nodeName := os.Getenv("NODE_NAME")
    envVar := os.Getenv("ENV_VAR") // This is the variable we will test overriding

    fmt.Printf("Hello from Go node: %s! The time is %s\n", nodeName, time.Now().Format("15:04:05"))
    
    // --- CALLING FUNCTION FROM UTILITY.GO ---
    message := GetCustomMessage(nodeName) 
    fmt.Println(message)
    // ---------------------------------------

    fmt.Printf("ENV_VAR (from execution): %s\n", envVar)

    if envVar == "success" {
        fmt.Println("--- Check 1: Success path activated (Exit 0) ---")
        os.Exit(0)
    } else {
        fmt.Printf("--- Check 2: Failure path activated (Exit 1) for ENV_VAR: %s ---\n", envVar)
        os.Exit(1)
    }
}
`,
			"hello/utility.go": `package main

import "fmt"

// GetCustomMessage generates a string, demonstrating a call across files.
func GetCustomMessage(name string) string {
    return fmt.Sprintf("A message generated by utility.go for node: %s (Successful multi-file compilation)", name)
}
`,
		},
		// Base environment variables set on the container itself (ENV_VAR will be overridden in run 2)
		Envs: []EnvironmentVariable{
			{Key: "NODE_NAME", Value: "GoWorkerInstance"},
			{Key: "ENV_VAR", Value: "initial_base_value"}, // This will be the value for the container, but overridden by execution envs
		},
	}

	workerImage := "golang:1.21-alpine"

	// Pull the image (omitting output for brevity)
	log.Printf("Ensuring image %s is available...", workerImage)
	reader, err := cli.ImagePull(ctx, workerImage, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", workerImage, err)
	}
	io.Copy(io.Discard, reader)

	// 4. Create and Start the Worker (one time setup)
	worker, err := NewWorkerFromSpec(ctx, cli, workerImage, spec)
	if err != nil {
		return err
	}
	defer worker.Stop(context.Background())

	// 5. Define Commands and Setup Output Streaming
	binaryPath := "/tmp/worker_binary"
	buildCommand := fmt.Sprintf("go build -o %s /tmp/hello/main.go /tmp/hello/utility.go", binaryPath)
	runCommand := binaryPath

	// Channels must be recreated if they are closed in a loop, but here we just need one set
	stdoutCh := make(chan string)
	stderrCh := make(chan string)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for line := range stdoutCh {
			fmt.Printf("[STDOUT] %s\n", line)
		}
	}()

	go func() {
		defer wg.Done()
		for line := range stderrCh {
			fmt.Printf("[STDERR] %s\n", line)
		}
	}()

	// --- STEP 1: BUILD (Compilation - only needs to happen once) ---
	log.Printf("Executing Build command: %s", buildCommand)
	// Build commands usually don't need exec-specific environment variables
	if execErr := worker.ExecuteCode(ctx, buildCommand, nil, stdoutCh, stderrCh); execErr != nil {
		// Compilation failed. Close channels and report the specific error.
		close(stdoutCh)
		close(stderrCh)
		wg.Wait()
		return fmt.Errorf("COMPILATION FAILED: %w", execErr)
	}
	log.Println("Build successful.")

	// --- RUN 1: Success Case (Overriding ENV_VAR to "success") ---
	log.Println("\n--- STARTING RUN 1: SUCCESS CASE ---")
	run1Envs := []EnvironmentVariable{
		{Key: "ENV_VAR", Value: "success"}, // Overrides the container's base 'initial_base_value'
	}
	execErr1 := worker.ExecuteCode(ctx, runCommand, run1Envs, stdoutCh, stderrCh)

	if execErr1 != nil {
		log.Printf("RUN 1 FAILED (Expected success): %v", execErr1)
	} else {
		log.Println("RUN 1: Successfully ran the code with dynamic ENV_VAR set to 'success'. Worker was not restarted.")
	}

	// --- RUN 2: Failure Case (Overriding ENV_VAR to "fail") ---
	log.Println("\n--- STARTING RUN 2: FAILURE CASE ---")
	run2Envs := []EnvironmentVariable{
		{Key: "ENV_VAR", Value: "fail"}, // Overrides the container's base 'initial_base_value' again
	}
	execErr2 := worker.ExecuteCode(ctx, runCommand, run2Envs, stdoutCh, stderrCh)

	if execErr2 != nil {
		log.Printf("RUN 2 FAILED (Expected failure, Exit 1): %v", execErr2)
	} else {
		return fmt.Errorf("RUN 2 unexpected success")
	}

	// Close channels and wait for output goroutines to finish
	close(stdoutCh)
	close(stderrCh)
	wg.Wait()

	log.Println("\nExample complete. The worker container was reused for two jobs with different environment variables.")
	return nil
}
