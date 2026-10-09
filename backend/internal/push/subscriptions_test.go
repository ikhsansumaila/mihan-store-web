package push

import (
	"testing"

	"mihanstore/internal/notify"
)

func TestPushKindsMatchNotify(t *testing.T) {
	if KindCreated != notify.KindCreated || KindCancelled != notify.KindCancelled {
		t.Fatal("jenis kejadian push harus sama dengan notify")
	}
}

func TestEndpointHashStable(t *testing.T) {
	a := EndpointHash("https://fcm.googleapis.com/fcm/send/x")
	if len(a) != 64 || a != EndpointHash("https://fcm.googleapis.com/fcm/send/x") || a == EndpointHash("https://fcm.googleapis.com/fcm/send/y") {
		t.Fatal("hash endpoint")
	}
}
