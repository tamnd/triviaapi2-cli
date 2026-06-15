package triviaapi2

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the domain wiring and pure string
// helpers. The client's HTTP behaviour is covered in triviaapi2_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "triviaapi2" {
		t.Errorf("Scheme = %q, want triviaapi2", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "triviaapi2" {
		t.Errorf("Identity.Binary = %q, want triviaapi2", info.Identity.Binary)
	}
}

func TestClassifyUnsupported(t *testing.T) {
	_, _, err := Domain{}.Classify("anything")
	if err == nil {
		t.Error("Classify: expected error for unsupported input, got nil")
	}
}

func TestLocateUnsupported(t *testing.T) {
	_, err := Domain{}.Locate("question", "abc")
	if err == nil {
		t.Error("Locate: expected error for unsupported type, got nil")
	}
}

func TestDomainRegister(t *testing.T) {
	// Verify the domain is registered via init() and kit.Open succeeds.
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}
	if h == nil {
		t.Fatal("kit.Open returned nil host")
	}
}
