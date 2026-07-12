package collector

import "sync"

// RingBuffer is a fixed-capacity, thread-safe circular buffer. Once full,
// adding a new element overwrites the oldest one. Used to hold a bounded
// amount of in-memory history per metric without unbounded growth or
// persistent storage.
type RingBuffer[T any] struct {
	mu       sync.Mutex
	data     []T
	capacity int
	next     int
	size     int
}

// NewRingBuffer creates a RingBuffer holding up to capacity elements.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer[T]{
		data:     make([]T, capacity),
		capacity: capacity,
	}
}

// Add appends a value, evicting the oldest value if the buffer is full.
func (r *RingBuffer[T]) Add(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data[r.next] = v
	r.next = (r.next + 1) % r.capacity
	if r.size < r.capacity {
		r.size++
	}
}

// Snapshot returns a copy of the buffered values in insertion order
// (oldest first).
func (r *RingBuffer[T]) Snapshot() []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]T, r.size)
	if r.size < r.capacity {
		copy(out, r.data[:r.size])
		return out
	}
	n := copy(out, r.data[r.next:])
	copy(out[n:], r.data[:r.next])
	return out
}
