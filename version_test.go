package mapperkit

import "testing"

func TestVersion(t *testing.T) {
	if got := Version(); got != version {
		t.Fatalf("Version() = %q, want %q", got, version)
	}
	if Version() == "" {
		t.Fatal("Version() must not be empty")
	}
}
