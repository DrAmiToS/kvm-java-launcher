package bmc

import "testing"

func TestDellST1FromBody(t *testing.T) {
	t.Parallel()

	body := []byte(`<root><authResult>0</authResult><forwardUrl>/start.html?ST1=f11e5e58688dd2e4e90e27af9005f5b4,ST2=bbbb</forwardUrl></root>`)
	got := dellST1FromBody(body)
	want := "f11e5e58688dd2e4e90e27af9005f5b4"
	if got != want {
		t.Fatalf("dellST1FromBody = %q, want %q", got, want)
	}
}

func TestDellAuthResultFromBody(t *testing.T) {
	t.Parallel()

	body := []byte(`<?xml version="1.0"?><root><authResult>99</authResult></root>`)
	got := dellAuthResultFromBody(body)
	if got != "99" {
		t.Fatalf("dellAuthResultFromBody = %q, want 99", got)
	}
	if msg := dellAuthResultMessage(got); msg == "" || msg == "unknown iDRAC login result" {
		t.Fatalf("dellAuthResultMessage(%q) = %q", got, msg)
	}
}

func TestDellLoginOriginUsesHostHeader(t *testing.T) {
	t.Parallel()

	client, err := New("https://90.189.216.197", "", "")
	if err != nil {
		t.Fatal(err)
	}
	client.SetHostHeader("192.168.7.70")

	got := dellLoginOrigin(client)
	want := "https://192.168.7.70"
	if got != want {
		t.Fatalf("dellLoginOrigin = %q, want %q", got, want)
	}
}
