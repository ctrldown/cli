package supervisor

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer("test-svc", 5)

	lines := []string{"line1", "line2", "line3", "line4", "line5", "line6", "line7"}
	for _, l := range lines {
		rb.Append(l)
	}

	result := rb.Lines()
	if len(result) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(result))
	}
	if result[0].Text != "line3" || result[4].Text != "line7" {
		t.Fatalf("unexpected buffer contents: %+v", result)
	}

	tail := rb.Tail(2)
	if len(tail) != 2 || tail[0].Text != "line6" || tail[1].Text != "line7" {
		t.Fatalf("unexpected tail: %+v", tail)
	}
}

func TestServiceLifecycle(t *testing.T) {
	cmdStr := "echo hello"
	if runtime.GOOS == "windows" {
		cmdStr = "echo hello"
	}

	svc := NewService(ServiceConfig{
		Name:     "echo-test",
		Category: CategoryBackend,
		Command:  cmdStr,
	})

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("failed to start service: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	lines := svc.Buffer.Lines()
	found := false
	for _, l := range lines {
		if strings.Contains(l.Text, "hello") || strings.Contains(l.Text, "Process started") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected output in buffer, got: %+v", lines)
	}

	_ = svc.Stop()
}
