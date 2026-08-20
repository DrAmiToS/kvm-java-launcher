package main

import (
	"net/url"
	"testing"

	"kvm-java-launcher/internal/bmc"
)

func TestResourceBaseUsesJNLPDirectory(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://bmc.example/")
	if err != nil {
		t.Fatal(err)
	}
	client := &bmc.Client{BaseURL: base}

	got := resourceBase(client, "https://bmc.example/cgi/url_redirect.cgi?url_name=ikvm&url_type=jnlp")
	want := "https://bmc.example/cgi/"
	if got != want {
		t.Fatalf("resourceBase = %q, want %q", got, want)
	}
}

func TestNormalizeVendorProfilesToGenericJNLP(t *testing.T) {
	t.Parallel()
	for _, profile := range []bmc.Profile{"dell", "hp", "hpe", "lenovo", "ibm", "generic"} {
		if got := normalizeProfile(profile); got != bmc.ProfileGenericJNLP {
			t.Fatalf("normalizeProfile(%q) = %q", profile, got)
		}
	}
}
