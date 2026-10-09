package orders

import (
	"fmt"
	"testing"
)

func TestMergeSuggestionsPriorityDedup(t *testing.T) {
	in := []ShippingSuggestion{
		{Source: "default", Fee: 15000},
		{Source: "district", Fee: 15000},
		{Source: "regency", Fee: 20000},
		{Source: "customer", Fee: 0},
		{Source: "customer", Fee: 20000},
	}
	out := mergeSuggestions(in)
	if len(out) != 2 || out[0].Source != "default" || fmt.Sprint(out[0].Sources) != "[default district]" ||
		out[1].Source != "regency" || fmt.Sprint(out[1].Sources) != "[regency customer]" {
		t.Fatalf("hasil: %+v", out)
	}
	many := []ShippingSuggestion{{Source: "a", Fee: 1}, {Source: "b", Fee: 2}, {Source: "c", Fee: 3}, {Source: "d", Fee: 4}, {Source: "e", Fee: 5}}
	if got := mergeSuggestions(many); len(got) != 4 {
		t.Fatalf("maks. 4: %d", len(got))
	}
	if got := mergeSuggestions([]ShippingSuggestion{{Source: "x", Fee: MaxShippingFee + 1}}); len(got) != 0 {
		t.Fatal("di luar batas harus dibuang")
	}
	if got := mergeSuggestions(nil); got == nil || len(got) != 0 {
		t.Fatal("kosong harus [] (bukan null)")
	}
}
