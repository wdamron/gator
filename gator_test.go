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

package gator

import (
	"testing"
	"unsafe"
)

func TestGator(t *testing.T) {
	tree := NewRegionTree()

	root, err := tree.NewRootRegion()
	if err != nil {
		t.Fatal(err)
	}

	sub, err := root.NewSubRegion()
	if err != nil {
		t.Fatal(err)
	}

	if !AssignPointer(sub, 0, root, 0) {
		t.Fatalf("failed to assign")
	}
	if *(*uintptr)(unsafe.Pointer(&sub.Mem[0])) == 0 {
		t.Fatalf("pointer value is nil")
	}
	if *(*uintptr)(unsafe.Pointer(&sub.Mem[0])) != uintptr(unsafe.Pointer(&root.Mem[0])) {
		t.Fatalf("pointer value is invalid")
	}

	if AssignPointer(root, 0, sub, 0) {
		t.Fatalf("uncaught invalid assignment")
	}

	if tree.FindRegion(&root.Mem[0]) != root {
		t.Fatalf("failed to find root region from memory pointer")
	}
	if tree.FindRegion(&sub.Mem[0]) != sub {
		t.Fatalf("failed to find sub region from memory pointer")
	}

	if err = root.Drop(); err == nil {
		t.Fatalf("root cannot be dropped before sub-regions are dropped")
	}
	if err = sub.Drop(); err != nil {
		t.Fatal(err)
	}
	if err = root.Drop(); err != nil {
		t.Fatal(err)
	}
	if tree.Root != nil || len(tree.index) != 0 {
		t.Fatal("regions were not dropped")
	}

	if unsafe.Sizeof(RegionMeta{}) != RegionMetaBytes {
		t.Fatalf("unexpected metadata size %v", unsafe.Sizeof(RegionMeta{}))
	}
	if unsafe.Sizeof(RegionHeader{}) != RegionHeaderBytes {
		t.Fatalf("unexpected header size %v", unsafe.Sizeof(RegionHeader{}))
	}
	if unsafe.Sizeof(Region{}) != RegionBytes {
		t.Fatalf("unexpected region size %v", unsafe.Sizeof(Region{}))
	}
}

func BenchmarkAssignPointerStackedRegions(b *testing.B) { // 6.34 ns/op
	tree := NewRegionTree()

	root, err := tree.NewRootRegion()
	if err != nil {
		b.Fatal(err)
	}

	sub, err := root.NewSubRegion()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		if !AssignPointer(sub, 0, root, 0) {
			b.Fatalf("failed to assign")
		}
	}
}

func BenchmarkAssignPointerSameRegion(b *testing.B) { // 5.70 ns/op
	tree := NewRegionTree()

	root, err := tree.NewRootRegion()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		if !AssignPointer(root, 0, root, 8) {
			b.Fatalf("failed to assign")
		}
	}
}
