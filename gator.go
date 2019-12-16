// +build amd64

// The MIT License (MIT)
//
// Copyright (c) 2019 West Damron
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// gator provides an unsafe region-based memory allocator for Go programs (amd64 only).
package gator

import (
	"errors"
	"unsafe"
)

const (
	RegionBytes       = 1024 * 256
	RegionHeaderBytes = 1024 * 4
	RegionMetaBytes   = 64
	RegionMemBytes    = (256 - 4) * 1024

	CellBytes = 8
	CellCount = ((256 - 4) * 1024) / 8 // 32256
)

type RegionFlags uint32

const (
	FlagStaticRegion = 1 + iota
	FlagHeapRegion
	FlagStackRegion
	FlagDroppedRegion
)

func (f RegionFlags) Static() bool  { return f == FlagStaticRegion }
func (f RegionFlags) Heap() bool    { return f == FlagHeapRegion }
func (f RegionFlags) Stack() bool   { return f == FlagStackRegion }
func (f RegionFlags) Dropped() bool { return f == FlagDroppedRegion }

type RegionTree struct {
	Root  *Region
	index []indexedRegion
}

type indexedRegion struct {
	min, max uintptr
	reg      *Region
}

func NewRegionTree() *RegionTree { return &RegionTree{} }

func (tree *RegionTree) NewRootRegion() (*Region, error) {
	root := &Region{}
	if err := tree.AddRootRegion(root); err != nil {
		return nil, err
	}
	return root, nil
}

func (tree *RegionTree) AddRootRegion(root *Region) error {
	if tree.Root != nil {
		return errors.New("root region already exists")
	}
	root.Header = RegionHeader{Meta: RegionMeta{
		Tree: tree,
	}}
	tree.Root = root
	tree.indexAdd(root)
	return nil
}

func (tree *RegionTree) FindRegion(mem *byte) *Region {
	idx, addr := tree.index, uintptr(unsafe.Pointer(mem))
	l, r := 0, len(idx)-1
	for l < r {
		m := int(uint(l+r) >> 1)
		if idx[m].max < addr {
			l = m + 1
		} else {
			r = m - 1
		}
	}
	if l == len(idx) || l == 0 && idx[l].min > addr {
		return nil
	}
	return idx[l].reg
}

func (tree *RegionTree) indexAdd(region *Region) {
	min := uintptr(unsafe.Pointer(&region.Mem[0]))
	max := min + RegionMemBytes - 1
	if len(tree.index) == 0 {
		tree.index = append(tree.index, indexedRegion{min, max, region})
		return
	}
	idx, l, r := tree.index, 0, len(tree.index)
	for l < r {
		m := int(uint(l+r) >> 1)
		idxmin := idx[m].min
		if idxmin < min {
			l = m + 1
		} else {
			r = m - 1
		}
	}
	if l == len(tree.index) {
		tree.index = append(tree.index, indexedRegion{min, max, region})
	} else {
		tree.index = append(tree.index, indexedRegion{})
		copy(tree.index[l+1:], tree.index[l:])
		tree.index[l] = indexedRegion{min, max, region}
	}
}

func (tree *RegionTree) indexDrop(region *Region) {
	min := uintptr(unsafe.Pointer(&region.Mem[0]))
	idx, l, r := tree.index, 0, len(tree.index)-1
	for l < r {
		m := int(uint(l+r) >> 1)
		idxmin := idx[m].min
		if idxmin < min {
			l = m + 1
		} else {
			r = m - 1
		}
	}
	if l != len(idx)-1 {
		copy(tree.index[l:], tree.index[l+1:])
	} else {
		tree.index[len(tree.index)-1].reg = nil
	}
	tree.index = tree.index[:len(tree.index)-1]
}

type RegionHeader struct {
	Meta RegionMeta
	// Bits may be used to store a boolean state for each cell in a region.
	Bits [CellCount / 64]uint64
}

type RegionMeta struct {
	Tree *RegionTree
	// Children form a doubly-linked list.
	Up, Left, Right, Down *Region
	Flags                 RegionFlags
	// Metadata extensions (e.g. stack pointer)
	Ext1 uint32
	Ext2 interface{}
}

func (h *RegionHeader) SetBit(index uint) {
	h.Bits[index/64] |= 1 << (index % 64)
}

func (h *RegionHeader) ClearBit(index uint) {
	h.Bits[index/64] &^= 1 << (index % 64)
}

type Region struct {
	Header RegionHeader
	Mem    [CellCount * CellBytes]byte
}

func NewRegion() *Region { return &Region{} }

func (r *Region) NewSubRegion() (*Region, error) {
	sub := &Region{}
	if err := r.AddSubRegion(sub); err != nil {
		return nil, err
	}
	return sub, nil
}

func (r *Region) AddSubRegion(sub *Region) error {
	rmeta := &r.Header.Meta
	if rmeta.Flags.Dropped() {
		return errors.New("parent region has already been dropped")
	}
	tree := rmeta.Tree
	sub.Header = RegionHeader{Meta: RegionMeta{
		Tree:  tree,
		Up:    r,
		Right: rmeta.Down,
		Flags: rmeta.Flags,
	}}
	if rmeta.Down != nil {
		rmeta.Down.Header.Meta.Left = sub
	}
	rmeta.Down = sub
	tree.indexAdd(sub)
	return nil
}

func (r *Region) Drop() error {
	meta := &r.Header.Meta
	if meta.Flags.Dropped() {
		return errors.New("region has already been dropped")
	}
	if meta.Down != nil {
		return errors.New("region cannot be dropped until all sub-regions are dropped")
	}
	meta.Flags |= FlagDroppedRegion
	tree, up, left, right := meta.Tree, meta.Up, meta.Left, meta.Right
	if r != tree.Root {
		up.Header.Meta.Down = right
		if left != nil {
			left.Header.Meta.Right = right
		}
		if right != nil {
			right.Header.Meta.Left = left
		}
	} else {
		tree.Root = nil
	}
	tree.indexDrop(r)
	return nil
}

func (r *Region) Tree() *RegionTree  { return r.Header.Meta.Tree }
func (r *Region) Up() *Region        { return r.Header.Meta.Up }
func (r *Region) Left() *Region      { return r.Header.Meta.Left }
func (r *Region) Right() *Region     { return r.Header.Meta.Right }
func (r *Region) Down() *Region      { return r.Header.Meta.Down }
func (r *Region) Flags() RegionFlags { return r.Header.Meta.Flags }

func (r *Region) Contains(mem *byte) bool {
	min := uintptr(unsafe.Pointer(&r.Mem[0]))
	max := min + RegionMemBytes - 1
	addr := uintptr(unsafe.Pointer(mem))
	return min <= addr && addr <= max
}

func AssignPointer(pointerRegion *Region, pointerMemOffset uint, pointsToRegion *Region, pointsToMemOffset uint) (ok bool) {
	if !CanAssignPointer(pointerRegion, pointsToRegion) {
		return false
	}
	UnsafeAssignPointer(&pointerRegion.Mem[pointerMemOffset], &pointsToRegion.Mem[pointsToMemOffset])
	return true
}

func UnsafeAssignPointer(mem, pointsTo *byte) {
	*((**byte)(unsafe.Pointer(mem))) = pointsTo
}

func CanAssignPointer(pointerRegion, pointsToRegion *Region) (ok bool) {
	if pointerRegion.Tree() != pointsToRegion.Tree() {
		return false
	}
	ptrFlags, memFlags := pointerRegion.Flags(), pointsToRegion.Flags()
	if ptrFlags.Dropped() || memFlags.Dropped() || (memFlags.Stack() && !ptrFlags.Stack()) {
		return false
	}
	if memFlags.Static() || pointerRegion == pointsToRegion {
		return true
	}
	for up := pointerRegion.Up(); up != nil; up = up.Up() {
		if up == pointsToRegion {
			return true
		}
	}
	return false
}
