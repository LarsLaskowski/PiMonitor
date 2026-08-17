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

func TestRingBuffer_Fill_RoundTrip(t *testing.T) {
	src := NewRingBuffer[int](3)
	for i := 1; i <= 5; i++ {
		src.Add(i)
	}

	dst := NewRingBuffer[int](3)
	dst.Fill(src.Snapshot())

	got := dst.Snapshot()
	want := []int{3, 4, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot after Fill = %v, want %v", got, want)
	}
}

func TestRingBuffer_Fill_KeepsNewestWhenOverCapacity(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Fill([]int{1, 2, 3, 4, 5})

	got := rb.Snapshot()
	want := []int{3, 4, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot after over-capacity Fill = %v, want %v", got, want)
	}
}

func TestRingBuffer_Fill_ThenAddContinues(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Fill([]int{1, 2})
	rb.Add(3)
	rb.Add(4)

	got := rb.Snapshot()
	want := []int{2, 3, 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot after Fill+Add = %v, want %v", got, want)
	}
}

func TestRingBuffer_Fill_ExactCapacityThenAdd(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Fill([]int{1, 2, 3})
	rb.Add(4)

	got := rb.Snapshot()
	want := []int{2, 3, 4}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot after full Fill+Add = %v, want %v", got, want)
	}
}

func TestRingBuffer_Fill_EmptyResets(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Add(1)
	rb.Fill(nil)

	if got := rb.Snapshot(); len(got) != 0 {
		t.Fatalf("expected empty snapshot after Fill(nil), got %v", got)
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	rb := NewRingBuffer[int](3)
	got := rb.Snapshot()
	if len(got) != 0 {
		t.Fatalf("expected empty snapshot, got %v", got)
	}
}

func TestRingBuffer_Newest_EmptyReturnsFalse(t *testing.T) {
	rb := NewRingBuffer[int](3)
	if _, ok := rb.Newest(); ok {
		t.Fatal("expected Newest to report false on an empty buffer")
	}
}

func TestRingBuffer_Newest_ReturnsLastAdded(t *testing.T) {
	rb := NewRingBuffer[int](3)
	rb.Add(1)
	rb.Add(2)
	if got, ok := rb.Newest(); !ok || got != 2 {
		t.Fatalf("Newest = (%v, %v), want (2, true)", got, ok)
	}
}

func TestRingBuffer_Newest_AfterWraparound(t *testing.T) {
	rb := NewRingBuffer[int](3)
	for i := 1; i <= 5; i++ {
		rb.Add(i)
	}
	if got, ok := rb.Newest(); !ok || got != 5 {
		t.Fatalf("Newest after wraparound = (%v, %v), want (5, true)", got, ok)
	}
}
