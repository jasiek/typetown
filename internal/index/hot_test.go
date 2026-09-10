package index

import "testing"

func TestTopKKeepsTheBest(t *testing.T) {
	tk := newTopK()
	for i := 0; i < hotK*3; i++ {
		tk.add(uint32(i), float64(i))
	}
	got := tk.drain()
	if len(got) != hotK {
		t.Fatalf("kept %d entries, want %d", len(got), hotK)
	}
	for i := 1; i < len(got); i++ {
		if got[i].score > got[i-1].score {
			t.Fatalf("entry %d scored %v, above entry %d at %v", i, got[i].score, i-1, got[i-1].score)
		}
	}
	if want := float64(hotK*3 - 1); got[0].score != want {
		t.Errorf("best score = %v, want %v", got[0].score, want)
	}
	if want := float64(hotK * 2); got[len(got)-1].score != want {
		t.Errorf("worst kept score = %v, want %v", got[len(got)-1].score, want)
	}
}

// A record reachable under one prefix by several names must appear once, at its
// best score — otherwise a popular place would crowd out the rest of the list.
func TestTopKDeduplicatesByOrdinal(t *testing.T) {
	tk := newTopK()
	tk.add(7, 1.0)
	tk.add(7, 9.0)
	tk.add(7, 4.0)
	tk.add(8, 2.0)

	got := tk.drain()
	if len(got) != 2 {
		t.Fatalf("kept %d entries, want 2 (%+v)", len(got), got)
	}
	if got[0].ord != 7 || got[0].score != 9.0 {
		t.Errorf("best = %+v, want ordinal 7 at 9.0", got[0])
	}
}

// The index it maintains has to survive the swaps a heap performs, or a later
// update lands on the wrong entry.
func TestTopKIndexSurvivesHeapMotion(t *testing.T) {
	tk := newTopK()
	for i := 0; i < hotK; i++ {
		tk.add(uint32(i), float64(hotK-i)) // descending: forces plenty of sifting
	}
	tk.add(uint32(hotK-1), 1000.0) // promote the entry that sits worst
	got := tk.drain()
	if got[0].ord != uint32(hotK-1) || got[0].score != 1000.0 {
		t.Errorf("best = %+v, want ordinal %d at 1000", got[0], hotK-1)
	}
	seen := map[uint32]bool{}
	for _, c := range got {
		if seen[c.ord] {
			t.Fatalf("ordinal %d appears twice", c.ord)
		}
		seen[c.ord] = true
	}
}

func TestTopKReset(t *testing.T) {
	tk := newTopK()
	tk.add(1, 5)
	tk.reset()
	if len(tk.drain()) != 0 || len(tk.pos) != 0 {
		t.Error("reset left state behind")
	}
	tk.add(2, 3)
	if got := tk.drain(); len(got) != 1 || got[0].ord != 2 {
		t.Errorf("after reset got %+v, want just ordinal 2", got)
	}
}

func TestCommonPrefixLen(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"london", "londrina", 4},
		{"paris", "paris", 5},
		{"a", "abc", 1},
		{"", "abc", 0},
		{"xyz", "abc", 0},
	}
	for _, tt := range tests {
		if got := commonPrefixLen([]byte(tt.a), []byte(tt.b)); got != tt.want {
			t.Errorf("commonPrefixLen(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
