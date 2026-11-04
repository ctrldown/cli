package supervisor

import (
	"bufio"
	"bytes"
	"strings"
	"sync"
)

// DefaultRingBufferSize is the maximum number of log lines retained per service.
const DefaultRingBufferSize = 2000

// LogLine represents a single timestamped log event with source tag.
type LogLine struct {
	Source string
	Text   string
}

// RingBuffer is a thread-safe circular buffer for streaming logs.
type RingBuffer struct {
	mu          sync.RWMutex
	source      string
	lines       []LogLine
	capacity    int
	start       int
	count       int
	subscribers map[chan LogLine]struct{}
}

// NewRingBuffer creates a ring buffer with the given capacity and service source identifier.
func NewRingBuffer(source string, capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = DefaultRingBufferSize
	}
	return &RingBuffer{
		source:      source,
		lines:       make([]LogLine, capacity),
		capacity:    capacity,
		subscribers: make(map[chan LogLine]struct{}),
	}
}

// Write implements io.Writer, scanning incoming byte chunks line by line.
func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(p))
	for scanner.Scan() {
		text := strings.TrimRight(scanner.Text(), "\r\n")
		if text != "" {
			rb.Append(text)
		}
	}
	return len(p), nil
}

// Append adds a new log line and broadcasts it to active TUI subscribers.
func (rb *RingBuffer) Append(text string) {
	rb.mu.Lock()
	entry := LogLine{
		Source: rb.source,
		Text:   text,
	}

	if rb.count < rb.capacity {
		rb.lines[rb.count] = entry
		rb.count++
	} else {
		rb.lines[rb.start] = entry
		rb.start = (rb.start + 1) % rb.capacity
	}

	// Copy active subscribers
	subs := make([]chan LogLine, 0, len(rb.subscribers))
	for ch := range rb.subscribers {
		subs = append(subs, ch)
	}
	rb.mu.Unlock()

	// Non-blocking broadcast
	for _, ch := range subs {
		select {
		case ch <- entry:
		default:
		}
	}
}

// Lines returns all currently stored lines in chronological order.
func (rb *RingBuffer) Lines() []LogLine {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]LogLine, rb.count)
	for i := 0; i < rb.count; i++ {
		idx := (rb.start + i) % rb.capacity
		result[i] = rb.lines[idx]
	}
	return result
}

// Tail returns the last n lines.
func (rb *RingBuffer) Tail(n int) []LogLine {
	all := rb.Lines()
	if len(all) <= n {
		return all
	}
	return all[len(all)-n:]
}

// Subscribe returns a channel that receives newly appended log lines.
func (rb *RingBuffer) Subscribe() chan LogLine {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	ch := make(chan LogLine, 100)
	rb.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a channel from the subscriber pool.
func (rb *RingBuffer) Unsubscribe(ch chan LogLine) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	delete(rb.subscribers, ch)
}
