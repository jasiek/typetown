package geonames

import "testing"

func TestFold(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases", "London", "london"},
		{"strips diacritics", "Rāmpur", "rampur"},
		{"strips several", "São Paulo", "sao paulo"},
		{"handles cedilla", "Eskişehir", "eskisehir"},
		{"collapses whitespace", "  New   York  ", "new york"},
		{"leaves non-latin alone", "Москва", "москва"},
		{"empty stays empty", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Fold(tt.in); got != tt.want {
				t.Errorf("Fold(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A query and the key it must match both pass through Fold, so folding has to
// be idempotent or the second pass would move the target.
func TestFoldIsIdempotent(t *testing.T) {
	for _, s := range []string{"München", "Ḩukūmat", "ÅLESUND", "Zürich"} {
		once := Fold(s)
		if twice := Fold(once); twice != once {
			t.Errorf("Fold(Fold(%q)) = %q, want %q", s, twice, once)
		}
	}
}

func TestCountScripts(t *testing.T) {
	tests := []struct {
		name string
		alts []string
		want int
	}{
		{"none", []string{"London", "Londres"}, 0},
		{"latin extended is still latin", []string{"Zürich", "Ćwikła"}, 0},
		{"distinct scripts count once each", []string{"Лондон", "Λονδίνο", "倫敦"}, 3},
		{"same script twice counts once", []string{"Лондон", "Москва"}, 1},
		{"han and kana are distinct", []string{"倫敦", "ロンドン"}, 2},
		{"empty", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countScripts(tt.alts); got != tt.want {
				t.Errorf("countScripts(%q) = %d, want %d", tt.alts, got, tt.want)
			}
		})
	}
}

// The signal exists to separate genuinely international places from names with
// many romanisations of a single script, which a raw alternate-name count cannot.
func TestCountScriptsIgnoresTransliterationVariance(t *testing.T) {
	romanisations := []string{"Ripsok", "Ripsŏk", "Rip-sok", "Ripsog", "Ripsŏg", "Ripseok"}
	international := []string{"Лондон", "Λονδίνο", "倫敦", "ロンドン", "لندن"}
	if got, want := countScripts(romanisations), 0; got != want {
		t.Errorf("countScripts(6 romanisations) = %d, want %d", got, want)
	}
	if got := countScripts(international); got != 5 {
		t.Errorf("countScripts(5 scripts) = %d, want 5", got)
	}
}
