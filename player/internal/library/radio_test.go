package library

import (
	"testing"

	"github.com/lubabs770/gooseneck/player/internal/ytm"
)

func TestNextSeedDrifts(t *testing.T) {
	st := RadioState{SeedID: "seed", Pool: []ytm.ArtistRef{{ID: "a"}, {ID: "b"}, {ID: "c"}}, Recent: []string{"a", "b"}}
	counts := map[string]int{}
	for i := 0; i < 600; i++ {
		counts[st.nextSeed()]++
	}
	if counts["a"] != 0 || counts["b"] != 0 {
		t.Fatalf("recent seeds reused: %v", counts)
	}
	if counts["seed"] < 100 || counts["c"] < 250 {
		t.Fatalf("expected ~1/3 original seed, rest fresh related: %v", counts)
	}
	if (&RadioState{SeedID: "only"}).nextSeed() != "only" {
		t.Fatal("empty pool must fall back to the seed")
	}
}

func TestLockFileIsExclusive(t *testing.T) {
	p := t.TempDir() + "/d.lock"
	rel, ok := lockFile(p)
	if !ok {
		t.Fatal("first lock failed")
	}
	if _, ok2 := lockFile(p); ok2 {
		t.Fatal("second lock should fail while the first is held")
	}
	rel()
	if rel2, ok3 := lockFile(p); !ok3 {
		t.Fatal("lock not released")
	} else {
		rel2()
	}
}
