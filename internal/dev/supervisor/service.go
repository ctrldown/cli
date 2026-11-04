package supervisor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// ServiceCategory defines which column of the 3-column bento grid the service belongs to.
type ServiceCategory string

const (
	CategoryInfra    ServiceCategory = "infra"    // Column 1: Docker containers & data stores
	CategoryBackend  ServiceCategory = "backend"  // Column 2: Go Echo APIs, workers, edge runtime
	CategoryFrontend ServiceCategory = "frontend" // Column 3: Next.js, Vite console, web apps
)

// ServiceStatus tracks the runtime lifecycle state of a service.
type ServiceStatus string

const (
	StatusStopped    ServiceStatus = "stopped"
	StatusStarting   ServiceStatus = "starting"
	StatusProbing    ServiceStatus = "probing"
	StatusRunning    ServiceStatus = "running"
	StatusCrashed    ServiceStatus = "crashed"
	StatusRestarting ServiceStatus = "restarting"
)

// ServiceConfig defines how a service is executed and monitored.
type ServiceConfig struct {
	Name          string
	Category      ServiceCategory
	Dir           string
	Command       string
	StopCommand   string
	Port          int
	HealthURL     string
	Env           []string
	DependsOn     []string
	AutoRestart   bool
	OneShotDaemon bool
}

// Service represents a monitored, supervised background development process.
type Service struct {
	mu           sync.RWMutex
	Config       ServiceConfig
	Status       ServiceStatus
	PID          int
	Restarts     int
	StartTime    time.Time
	Buffer       *RingBuffer
	cmd          *exec.Cmd
	processGroup *ProcessGroup
	cancelFunc   context.CancelFunc
	doneChan     chan struct{}
	lastError    error
}

// NewService instantiates a supervised service.
func NewService(cfg ServiceConfig) *Service {
	return &Service{
		Config: cfg,
		Status: StatusStopped,
		Buffer: NewRingBuffer(cfg.Name, DefaultRingBufferSize),
	}
}

// Start spawns the service process and streams logs to its ring buffer.
func (s *Service) Start(parentCtx context.Context) error {
	s.mu.Lock()
	if s.Status == StatusRunning || s.Status == StatusStarting {
		s.mu.Unlock()
		return fmt.Errorf("service %s is already running", s.Config.Name)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	s.cancelFunc = cancel
	s.doneChan = make(chan struct{})
	s.Status = StatusStarting
	s.StartTime = time.Now()
	s.mu.Unlock()

	// Prepare shell command based on OS
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", s.Config.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", s.Config.Command)
	}

	cmd.Dir = s.Config.Dir
	cmd.Env = append(os.Environ(), s.Config.Env...)

	// OS Process Group isolation
	pg, err := NewProcessGroup()
	if err == nil {
		pg.PrepareCmd(cmd)
		s.processGroup = pg
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		s.setStatus(StatusCrashed, err)
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		s.setStatus(StatusCrashed, err)
		return err
	}

	// Pre-flight port check: warn if port is already occupied before process starts
	if s.Config.Port > 0 && !s.Config.OneShotDaemon {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.Config.Port), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			s.Buffer.Append(fmt.Sprintf("⚠ [%s] Port %d is already in use by another process/container!", s.Config.Name, s.Config.Port))
		}
	}

	if err := cmd.Start(); err != nil {
		s.setStatus(StatusCrashed, err)
		return fmt.Errorf("failed to start %s: %w", s.Config.Name, err)
	}

	s.mu.Lock()
	s.cmd = cmd
	s.PID = cmd.Process.Pid
	if s.Config.Port > 0 || s.Config.HealthURL != "" {
		s.Status = StatusProbing
	} else {
		s.Status = StatusRunning
	}
	s.mu.Unlock()

	// Attach to Windows Job Object
	if s.processGroup != nil {
		_ = s.processGroup.Attach(cmd)
	}

	s.Buffer.Append(fmt.Sprintf("⚡ [%s] Process started (PID: %d)", s.Config.Name, s.PID))

	// Stream stdout & stderr
	go func() {
		_, _ = io.Copy(s.Buffer, stdoutPipe)
	}()
	go func() {
		_, _ = io.Copy(s.Buffer, stderrPipe)
	}()

	// Start health probe
	go s.runHealthProbe(ctx)

	// Wait for process termination
	go func() {
		err := cmd.Wait()
		duration := time.Since(s.StartTime)
		s.mu.Lock()
		close(s.doneChan)
		wasKilled := ctx.Err() != nil
		if wasKilled {
			s.Status = StatusStopped
		} else if err != nil {
			s.Status = StatusCrashed
			s.lastError = err
			s.Buffer.Append(fmt.Sprintf("❌ [%s] Process exited with error: %v", s.Config.Name, err))
		} else if s.Config.OneShotDaemon {
			s.Status = StatusRunning
			s.Buffer.Append(fmt.Sprintf("✓ [%s] Daemon launched in background", s.Config.Name))
			go s.monitorDaemonPort(ctx)
		} else {
			s.Status = StatusStopped
			s.Buffer.Append(fmt.Sprintf("⏹ [%s] Process exited cleanly (code 0)", s.Config.Name))
		}
		s.PID = 0
		s.mu.Unlock()

		// Optional auto-restart on unexpected crash with CrashLoopBackOff protection
		if !wasKilled && s.Config.AutoRestart {
			if s.Restarts >= 3 && duration < 3*time.Second {
				s.Buffer.Append(fmt.Sprintf("🛑 [%s] Crash loop detected (failed %d times rapidly). Auto-restart paused. Fix port conflict or press [r] to retry.", s.Config.Name, s.Restarts+1))
				s.mu.Lock()
				s.Status = StatusCrashed
				s.mu.Unlock()
				return
			}

			backoff := time.Duration(1<<s.Restarts) * 500 * time.Millisecond
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
			time.Sleep(backoff)
			s.mu.Lock()
			s.Restarts++
			s.Status = StatusRestarting
			s.mu.Unlock()
			s.Buffer.Append(fmt.Sprintf("🔄 [%s] Auto-restarting (attempt %d)...", s.Config.Name, s.Restarts))
			_ = s.Start(parentCtx)
		}
	}()

	return nil
}

// Stop gracefully terminates the service process.
func (s *Service) Stop() error {
	s.mu.Lock()
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	cmd := s.cmd
	pg := s.processGroup
	done := s.doneChan
	stopCmd := s.Config.StopCommand
	dir := s.Config.Dir
	s.mu.Unlock()

	// Execute explicit StopCommand (e.g. docker compose stop) if configured
	if stopCmd != "" {
		var c *exec.Cmd
		if runtime.GOOS == "windows" {
			c = exec.Command("cmd.exe", "/c", stopCmd)
		} else {
			c = exec.Command("sh", "-c", stopCmd)
		}
		c.Dir = dir
		_ = c.Run()
	}

	if cmd != nil && cmd.Process != nil {
		if pg != nil {
			_ = pg.Kill(cmd.Process)
		} else {
			_ = cmd.Process.Kill()
		}
	}

	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	s.mu.Lock()
	s.Status = StatusStopped
	s.PID = 0
	s.mu.Unlock()
	return nil
}

// Restart stops and restarts the service cleanly.
func (s *Service) Restart(ctx context.Context) error {
	s.Buffer.Append(fmt.Sprintf("🔄 [%s] Manual restart requested...", s.Config.Name))
	_ = s.Stop()
	time.Sleep(300 * time.Millisecond)
	s.mu.Lock()
	s.Restarts = 0
	s.mu.Unlock()
	return s.Start(ctx)
}

func (s *Service) setStatus(status ServiceStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	s.lastError = err
}

// runHealthProbe checks TCP port or HTTP endpoint until available.
func (s *Service) runHealthProbe(ctx context.Context) {
	if s.Config.Port <= 0 && s.Config.HealthURL == "" {
		return
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(45 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout:
			s.Buffer.Append(fmt.Sprintf("⚠ [%s] Health probe timed out after 45s", s.Config.Name))
			return
		case <-ticker.C:
			healthy := false
			if s.Config.HealthURL != "" {
				client := &http.Client{Timeout: 1 * time.Second}
				resp, err := client.Get(s.Config.HealthURL)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode < 500 {
						healthy = true
					}
				}
			} else if s.Config.Port > 0 {
				conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.Config.Port), 400*time.Millisecond)
				if err == nil {
					conn.Close()
					healthy = true
				}
			}

			if healthy {
				s.mu.Lock()
				if s.Status == StatusProbing || s.Status == StatusStarting {
					s.Status = StatusRunning
					s.Buffer.Append(fmt.Sprintf("✓ [%s] Health probe passed (Ready on port %d)", s.Config.Name, s.Config.Port))
				}
				s.mu.Unlock()
				return
			}
		}
	}
}

// monitorDaemonPort periodically checks if a background daemon port is still listening.
func (s *Service) monitorDaemonPort(ctx context.Context) {
	if s.Config.Port <= 0 {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.Config.Port), 400*time.Millisecond)
			s.mu.Lock()
			if err == nil {
				conn.Close()
				s.Status = StatusRunning
			} else if s.Status == StatusRunning {
				s.Status = StatusStopped
				s.Buffer.Append(fmt.Sprintf("⚠ [%s] Daemon port %d stopped responding", s.Config.Name, s.Config.Port))
			}
			s.mu.Unlock()
		}
	}
}

// Snapshot returns a thread-safe copy of the service's current state.
func (s *Service) Snapshot() (status ServiceStatus, pid int, uptime time.Duration, restarts int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	up := time.Duration(0)
	if s.Status == StatusRunning || s.Status == StatusProbing {
		up = time.Since(s.StartTime)
	}
	return s.Status, s.PID, up, s.Restarts
}
