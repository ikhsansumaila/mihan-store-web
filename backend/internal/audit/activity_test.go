package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeDetails(t *testing.T) {
	d := map[string]any{
		"password": "rahasia", "newPassword": "x", "password_hash": "$argon2id$...", "token": "abc",
		"Authorization": "Bearer abc", "cookie": "CF_Authorization=xyz", "id_token": "eyJ...", "credential": "eyJ...",
		"profileToken": "p", "nested": map[string]any{"sessionToken": "s", "aman": "ok"},
		"panjang": strings.Repeat("a", 1000), "harga": 15000,
		"perubahan": map[string]any{"price": map[string]any{"dari": 1, "menjadi": 2}},
	}
	js := *detailsJSON(d)
	for _, secret := range []string{"rahasia", "argon2id", "abc", "xyz", "eyJ", `"p"`, `"s"`} {
		if strings.Contains(js, secret) {
			t.Errorf("nilai sensitif %q bocor ke log: %s", secret, js)
		}
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(js), &back); err != nil {
		t.Fatal(err)
	}
	if back["harga"].(float64) != 15000 || back["nested"].(map[string]any)["aman"] != "ok" {
		t.Error("nilai aman harus tetap ada")
	}
	if n := len([]rune(back["panjang"].(string))); n > maxDetailString+1 {
		t.Errorf("nilai panjang tidak dipotong: %d", n)
	}
	if back["perubahan"].(map[string]any)["price"] == nil {
		t.Error("selisih perubahan harus tetap ada")
	}
	// Ukuran total dibatasi.
	huge := map[string]any{}
	for i := 0; i < 49; i++ {
		huge[strings.Repeat("k", 10)+string(rune('a'+i%26))+string(rune('a'+i/26))] = strings.Repeat("x", 300)
	}
	if s := detailsJSON(huge); s == nil || len(*s) > maxDetailBytes {
		t.Error("details besar harus dipotong")
	}
	// Entry: summary/actor/UA dipotong sesuai kolom.
	row := Entry{Action: "auth.login_failed", ActorLabel: strings.Repeat("A", 300), UserAgent: strings.Repeat("u", 600)}.toRow()
	if len(*row.ActorLabel) > 100 || len(*row.UserAgent) > 255 || row.Summary == "" {
		t.Error("kolom log tidak dipotong dengan benar")
	}
}

func TestDiffMaps(t *testing.T) {
	d := DiffMaps(map[string]any{"price": uint32(1000), "name": "A"}, map[string]any{"price": uint32(2000), "name": "A"})
	if len(d) != 1 || d["price"] == nil {
		t.Fatalf("diff salah: %v", d)
	}
}
