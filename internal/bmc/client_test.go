package bmc

import "testing"

func TestClientURLPreservesQueryString(t *testing.T) {
	t.Parallel()
	client, err := New("http://10.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	got := client.URL("/cgi/url_redirect.cgi?url_name=sol&url_type=jwss")
	want := "http://10.0.0.1/cgi/url_redirect.cgi?url_name=sol&url_type=jwss"
	if got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}
