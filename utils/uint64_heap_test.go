package utils

import (
	"container/heap"
	"testing"
)

func TestUint64Heap(t *testing.T) {
	h := &UInt64Heap{}
	heap.Init(h)
	heap.Push(h, uint64(1))
	heap.Push(h, uint64(2))
	heap.Push(h, uint64(3))
	heap.Push(h, uint64(4))

	if (*h).Peek() != uint64(1) {
		t.Fatalf("Expected %d but got %d", uint64(1), (*h).Peek())
	}

	if heap.Pop(h) != uint64(1) {
		t.Fatalf("Expected %d but got %d", uint64(1), heap.Pop(h))
	}

	if (*h).Peek() != uint64(2) {
		t.Fatalf("Expected %d but got %d", uint64(2), (*h).Peek())
	}

	if heap.Pop(h) != uint64(2) {
		t.Fatalf("Expected %d but got %d", uint64(2), heap.Pop(h))
	}

	if (*h).Peek() != uint64(3) {
		t.Fatalf("Expected %d but got %d", uint64(3), (*h).Peek())
	}

	if heap.Pop(h) != uint64(3) {
		t.Fatalf("Expected %d but got %d", uint64(3), heap.Pop(h))
	}

	if (*h).Peek() != uint64(4) {
		t.Fatalf("Expected %d but got %d", uint64(4), (*h).Peek())
	}

	if heap.Pop(h) != uint64(4) {
		t.Fatalf("Expected %d but got %d", uint64(4), heap.Pop(h))
	}
}
