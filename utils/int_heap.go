// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

// UInt64Heap adapted from https://pkg.go.dev/container/heap#example-package-IntHeap
type UInt64Heap []uint64

func (h UInt64Heap) Len() int           { return len(h) }
func (h UInt64Heap) Less(i, j int) bool { return h[i] < h[j] }
func (h UInt64Heap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h UInt64Heap) Peek() uint64       { return h[0] }

func (h *UInt64Heap) Push(x any) {
	// Push and Pop use pointer receivers because they modify the slice's length,
	// not just its contents.
	*h = append(*h, x.(uint64))
}

func (h *UInt64Heap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
