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

func TestNormalizeProfiles(t *testing.T) {
	t.Parallel()
	cases := map[bmc.Profile]bmc.Profile{
		"dell":    bmc.ProfileDell,
		"hp":      bmc.ProfileHP,
		"hpe":     bmc.ProfileHP,
		"lenovo":  bmc.ProfileLenovo,
		"ibm":     bmc.ProfileLenovo,
		"generic": bmc.ProfileGenericJNLP,
	}
	for profile, want := range cases {
		if got := normalizeProfile(profile); got != want {
			t.Fatalf("normalizeProfile(%q) = %q, want %q", profile, got, want)
		}
	}
}
