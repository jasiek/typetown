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
