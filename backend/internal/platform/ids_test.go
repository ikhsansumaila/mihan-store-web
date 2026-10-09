package platform

import (
	"strings"
	"testing"
)

func TestPublicID(t *testing.T) {
	id, err := NewPublicID()
	if err != nil || len(id) != 36 || id[14] != '4' || strings.Count(id, "-") != 4 {
		t.Fatalf("UUID tidak valid: %q", id)
	}
}
