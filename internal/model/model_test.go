package model

import (
	"testing"
	"time"
)

func TestPercentAndETA(t *testing.T) {
	d := &Download{Size: 100, Done: 25, Speed: 10}
	if p := d.Percent(); p != 0.25 {
		t.Errorf("percent = %v", p)
	}
	if eta := d.ETA(); eta < 7*time.Second || eta > 9*time.Second {
		t.Errorf("eta = %v", eta)
	}
	d.Done = 100
	if d.Percent() != 1 {
		t.Errorf("complete percent = %v", d.Percent())
	}
	if d.ETA() != 0 {
		t.Errorf("complete eta = %v", d.ETA())
	}
}

func TestCategoryMatch(t *testing.T) {
	c := Category{Types: []string{"ZIP", ".rar", "*"}}
	if !c.Match("a.zip") || !c.Match("b.rar") || !c.Match("c.anything") {
		t.Error("a type did not match")
	}
	g := Category{Types: []string{"pdf"}}
	if g.Match("a.zip") {
		t.Error("pdf matched a zip")
	}
}

func TestSortDownloads(t *testing.T) {
	list := []*Download{
		{FileName: "b", Size: 2},
		{FileName: "a", Size: 3},
		{FileName: "c", Size: 1},
	}
	byName := SortDownloads(list, 0, false)
	if byName[0].FileName != "a" || byName[2].FileName != "c" {
		t.Errorf("by name: %v", names(byName))
	}
	bySize := SortDownloads(list, 1, true)
	if bySize[0].Size != 3 {
		t.Errorf("by size desc: %v", names(bySize))
	}
	// The original order is untouched.
	if list[0].FileName != "b" {
		t.Error("SortDownloads changed the input")
	}
}

func names(list []*Download) []string {
	out := make([]string, len(list))
	for i, d := range list {
		out[i] = d.FileName
	}
	return out
}
