package platform

import (
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	for in, want := range map[string]string{"": DefaultPublicBaseURL, "https://store.mihan.web.id/": "https://store.mihan.web.id",
		"javascript:alert(1)": DefaultPublicBaseURL, "https://a.b/?x=1": DefaultPublicBaseURL, "http://localhost:3000": "http://localhost:3000"} {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("NormalizeBaseURL(%q) = %q", in, got)
		}
	}
}

func TestRegionAPIBaseFromEnv(t *testing.T) {
	cases := []struct {
		in   string
		test bool
		want string
	}{
		{"", false, DefaultRegionAPIBase},
		{"https://wilayah.id/api/", false, "https://wilayah.id/api"},
		{"http://mock:9000/wilayah/api", false, DefaultRegionAPIBase},
		{"https://evil.example/api", false, DefaultRegionAPIBase},
		{"http://mock:9000/wilayah/api", true, "http://mock:9000/wilayah/api"},
		{"ftp://x/api", true, DefaultRegionAPIBase},
		{"https://user@wilayah.id/api", false, DefaultRegionAPIBase},
	}
	for _, c := range cases {
		if got := RegionAPIBaseFromEnv(c.in, c.test); got != c.want {
			t.Errorf("REGION_API_BASE %q test=%v -> %q, mau %q", c.in, c.test, got, c.want)
		}
	}
}
