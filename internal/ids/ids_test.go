package ids

import "testing"

func TestValidAcceptsGeneratedUUIDAndRejectsUntrustedIdentifiers(t *testing.T) {
	if value := New(); !Valid(value) {
		t.Fatalf("generated UUID rejected: %q", value)
	}
	for _, value := range []string{"", "not-a-uuid", "../../metrics", "550e8400-e29b-11d4-a716-446655440000", "550E8400-E29B-41D4-A716-446655440000"} {
		if Valid(value) {
			t.Fatalf("invalid identifier accepted: %q", value)
		}
	}
}
