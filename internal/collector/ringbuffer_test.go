package collector

import (
	"reflect"
	"testing"
)

func TestRingBuffer_BasicOrder(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Add(1)
	rb.Add(2)
	got := rb.Snapshot()
	want := []int{1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %v, want %v", got, want)
	}
}

func TestRingBuffer_Wraparound(t *testing.T) {
	rb := NewRingBuffer[int](3)
	for i := 1; i <= 5; i++ {
		rb.Add(i)
	}
	got := rb.Snapshot()
	want := []int{3, 4, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot after wraparound = %v, want %v", got, want)
	}
}

func TestRingBuffer_CapacityEnforced(t *testing.T) {
	rb := NewRingBuffer[int](2)
	for i := 0; i < 10; i++ {
		rb.Add(i)
	}
	got := rb.Snapshot()
	if len(got) != 2 {
		t.Fatalf("expected capacity-bounded length 2, got %d", len(got))
	}
}

func TestRingBuffer_MinimumCapacityOne(t *testing.T) {
	rb := NewRingBuffer[int](0)
	rb.Add(42)
	got := rb.Snapshot()
	want := []int{42}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot = %v, want %v", got, want)
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	rb := NewRingBuffer[int](3)
	got := rb.Snapshot()
	if len(got) != 0 {
		t.Fatalf("expected empty snapshot, got %v", got)
	}
}
