package platform

import "testing"

func TestVAPIDSubjectDefault(t *testing.T) {
	t.Setenv("VAPID_SUBJECT", "")
	t.Setenv("PUBLIC_BASE_URL", "")
	if c := LoadConfig(); c.VAPIDSubject != "https://store.mihan.web.id" {
		t.Fatalf("subject default: %q", c.VAPIDSubject)
	}
	t.Setenv("VAPID_SUBJECT", "mailto:toko@mihan.web.id")
	if c := LoadConfig(); c.VAPIDSubject != "mailto:toko@mihan.web.id" {
		t.Fatalf("subject: %q", c.VAPIDSubject)
	}
}
