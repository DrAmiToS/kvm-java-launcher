package main

import (
	"net/url"
	"testing"

	"kvm-java-launcher/internal/bmc"
	"kvm-java-launcher/internal/jnlp"
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

func TestBypassSupermicroStunnel(t *testing.T) {
	t.Parallel()
	parsed := &jnlp.File{
		Main: "tw.com.aten.ikvm.KVMMain",
		Args: []string{"192.168.33.13", "user", "pass", "null", "63630", "63631", "0", "0", "1", "5900", "623"},
	}

	if !bypassSupermicroStunnel(parsed) {
		t.Fatal("expected stunnel bypass")
	}
	if len(parsed.Args) != 11 || parsed.Args[4] != "5900" || parsed.Args[5] != "623" || parsed.Args[6] != "2" || parsed.Args[7] != "0" || parsed.Args[8] != "0" || parsed.Args[9] != "5900" || parsed.Args[10] != "623" {
		t.Fatalf("args were not rewritten: %#v", parsed.Args)
	}
}

func TestBypassSupermicroStunnelLeavesOldFormat(t *testing.T) {
	t.Parallel()
	parsed := &jnlp.File{
		Main: "tw.com.aten.ikvm.KVMMain",
		Args: []string{"10.168.68.3", "user", "pass", "null", "5900", "623", "2", "0"},
	}

	if bypassSupermicroStunnel(parsed) {
		t.Fatal("old direct format should not be rewritten")
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
