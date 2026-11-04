package supervisor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Supervisor coordinates the lifecycle of all services across the 3 categories.
type Supervisor struct {
	mu           sync.RWMutex
	services     []*Service
	serviceMap   map[string]*Service
	unifiedLogCh chan LogLine
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewSupervisor instantiates a multi-process supervisor.
func NewSupervisor(configs []ServiceConfig) *Supervisor {
	sup := &Supervisor{
		serviceMap:   make(map[string]*Service),
		unifiedLogCh: make(chan LogLine, 500),
	}

	for _, cfg := range configs {
		svc := NewService(cfg)
		sup.services = append(sup.services, svc)
		sup.serviceMap[cfg.Name] = svc

		// Forward logs to unified feed
		ch := svc.Buffer.Subscribe()
		go func(s *Service, incoming chan LogLine) {
			for line := range incoming {
				select {
				case sup.unifiedLogCh <- line:
				default:
				}
			}
		}(svc, ch)
	}

	return sup
}

// Services returns all registered services.
func (sup *Supervisor) Services() []*Service {
	sup.mu.RLock()
	defer sup.mu.RUnlock()
	out := make([]*Service, len(sup.services))
	copy(out, sup.services)
	return out
}

// ByCategory filters services by their column category.
func (sup *Supervisor) ByCategory(cat ServiceCategory) []*Service {
	sup.mu.RLock()
	defer sup.mu.RUnlock()
	var out []*Service
	for _, s := range sup.services {
		if s.Config.Category == cat {
			out = append(out, s)
		}
	}
	return out
}

// Get finds a service by name.
func (sup *Supervisor) Get(name string) *Service {
	sup.mu.RLock()
	defer sup.mu.RUnlock()
	return sup.serviceMap[name]
}

// UnifiedLogChannel provides a combined stream of log lines from all running services.
func (sup *Supervisor) UnifiedLogChannel() <-chan LogLine {
	return sup.unifiedLogCh
}

// StartAll launches services in staged order: Infra -> Backend -> Frontend.
func (sup *Supervisor) StartAll(parentCtx context.Context) error {
	sup.mu.Lock()
	sup.ctx, sup.cancel = context.WithCancel(parentCtx)
	ctx := sup.ctx
	sup.mu.Unlock()

	// Stage 1: Infra (Docker, databases)
	infra := sup.ByCategory(CategoryInfra)
	for _, s := range infra {
		if err := s.Start(ctx); err != nil {
			sup.unifiedLogCh <- LogLine{Source: "system", Text: fmt.Sprintf("Failed to start %s: %v", s.Config.Name, err)}
		}
	}
	if len(infra) > 0 {
		time.Sleep(1500 * time.Millisecond)
	}

	// Stage 2: Backend (Go Echo, edge workers)
	backend := sup.ByCategory(CategoryBackend)
	for _, s := range backend {
		if err := s.Start(ctx); err != nil {
			sup.unifiedLogCh <- LogLine{Source: "system", Text: fmt.Sprintf("Failed to start %s: %v", s.Config.Name, err)}
		}
	}
	if len(backend) > 0 {
		time.Sleep(800 * time.Millisecond)
	}

	// Stage 3: Frontend (Next.js, Vite console)
	frontend := sup.ByCategory(CategoryFrontend)
	for _, s := range frontend {
		if err := s.Start(ctx); err != nil {
			sup.unifiedLogCh <- LogLine{Source: "system", Text: fmt.Sprintf("Failed to start %s: %v", s.Config.Name, err)}
		}
	}

	return nil
}

// StopAll stops all running services in reverse order.
func (sup *Supervisor) StopAll() {
	sup.mu.Lock()
	if sup.cancel != nil {
		sup.cancel()
	}
	services := make([]*Service, len(sup.services))
	copy(services, sup.services)
	sup.mu.Unlock()

	var wg sync.WaitGroup
	// Stop frontend and backend in parallel, then infra
	for i := len(services) - 1; i >= 0; i-- {
		s := services[i]
		wg.Add(1)
		go func(svc *Service) {
			defer wg.Done()
			_ = svc.Stop()
		}(s)
	}
	wg.Wait()
}

// RestartService triggers a clean restart for the specified service.
func (sup *Supervisor) RestartService(name string) error {
	s := sup.Get(name)
	if s == nil {
		return fmt.Errorf("service %s not found", name)
	}
	sup.mu.RLock()
	ctx := sup.ctx
	sup.mu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	return s.Restart(ctx)
}
