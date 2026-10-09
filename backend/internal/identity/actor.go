package identity

// Identitas pelaku untuk activity log (internal/audit.Entry).

func UID(u *User) *uint64 {
	if u == nil {
		return nil
	}
	id := u.ID
	return &id
}

func ActorOf(u *User) string {
	if u == nil {
		return ""
	}
	if u.Email != "" {
		return u.Email
	}
	return u.Username
}
