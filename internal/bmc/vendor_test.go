package bmc

import (
	"strings"
	"testing"
)

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

func TestHPSessionKeyFromBody(t *testing.T) {
	t.Parallel()

	body := []byte(`{"session_key":"0123456789abcdef0123456789abcdef"}`)
	got := hpSessionKeyFromBody(body)
	want := "0123456789abcdef0123456789abcdef"
	if got != want {
		t.Fatalf("hpSessionKeyFromBody = %q, want %q", got, want)
	}
}

func TestFingerprintBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want Profile
	}{
		{
			name: "supermicro aten page",
			body: `<html><meta name="ATEN International Co Ltd."><script src="../js/lang/en/lang_str.js"></script><a href="/cgi/url_redirect.cgi?url_name=ikvm&url_type=jwsk">KVM</a></html>`,
			want: ProfileSupermicro,
		},
		{
			name: "ami megarac",
			body: `<html>American Megatrends MegaRAC JViewer.jnlp</html>`,
			want: ProfileAMIMegaRAC,
		},
		{
			name: "dell idrac",
			body: `<html><title>iDRAC</title><form action="/data/login"></form>PowerEdge</html>`,
			want: ProfileDell,
		},
		{
			name: "hpe ilo",
			body: `<html>HPE iLO Integrated Lights-Out java_irc.html intgapp</html>`,
			want: ProfileHP,
		},
		{
			name: "lenovo xclarity",
			body: `<html>Lenovo XClarity remote console</html>`,
			want: ProfileLenovo,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := fingerprintBody([]byte(tc.body))
			if !ok || got != tc.want {
				t.Fatalf("fingerprintBody = %q, %v; want %q, true", got, ok, tc.want)
			}
		})
	}
}

func TestHPLicenseFromBody(t *testing.T) {
	t.Parallel()

	license, err := hpLicenseFromBody([]byte(`{"license":"iLO Advanced"}`))
	if err != nil {
		t.Fatal(err)
	}
	if license != "iLO Advanced" {
		t.Fatalf("hpLicenseFromBody = %q, want iLO Advanced", license)
	}
}

func TestHPAppletJNLP(t *testing.T) {
	t.Parallel()

	client, err := New("https://ilo.example", "administrator", "password")
	if err != nil {
		t.Fatal(err)
	}
	body := hpAppletJNLP(client, "abc123", "/html/intgapp4_231.jar", "en", false)
	for _, want := range []string{
		`codebase="https://ilo.example/"`,
		`href="html/intgapp4_231.jar"`,
		`main-class="com.hp.ilo2.intgapp.intgapp"`,
		`documentbase="https://ilo.example/html/java_irc.html?lang=en"`,
		`name="RCINFO1" value="abc123"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("generated JNLP missing %q:\n%s", want, body)
		}
	}
}

func TestHPAppletJNLPHTTPDocumentBase(t *testing.T) {
	t.Parallel()

	client, err := New("https://ilo.example", "administrator", "password")
	if err != nil {
		t.Fatal(err)
	}
	body := hpAppletJNLP(client, "abc123", "/html/intgapp3_231.jar", "en", true)
	for _, want := range []string{
		`codebase="https://ilo.example/"`,
		`href="html/intgapp3_231.jar"`,
		`documentbase="http://ilo.example/html/java_irc.html?lang=en"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("generated JNLP missing %q:\n%s", want, body)
		}
	}
}

func TestHPDefaultJavaIRCJar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		license string
		want    string
	}{
		{license: "iLO 2 Advanced", want: "/html/intgapp2_231.jar"},
		{license: "iLO 3 Advanced", want: "/html/intgapp3_231.jar"},
		{license: "iLO Advanced", want: "/html/intgapp4_231.jar"},
		{license: "", want: "/html/intgapp4_231.jar"},
	}
	for _, tc := range tests {
		t.Run(tc.license, func(t *testing.T) {
			if got := hpDefaultJavaIRCJar(tc.license); got != tc.want {
				t.Fatalf("hpDefaultJavaIRCJar(%q) = %q, want %q", tc.license, got, tc.want)
			}
		})
	}
}

func TestHPLegacyAppletMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		license string
		want    bool
	}{
		{license: "iLO 2 Advanced", want: true},
		{license: "iLO 3 Advanced", want: true},
		{license: "iLO Advanced", want: false},
		{license: "iLO 4 Advanced", want: false},
		{license: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.license, func(t *testing.T) {
			if got := hpLegacyAppletMode(tc.license); got != tc.want {
				t.Fatalf("hpLegacyAppletMode(%q) = %v, want %v", tc.license, got, tc.want)
			}
		})
	}
}

func TestHPJavaIRCJarFromHTML(t *testing.T) {
	t.Parallel()

	body := []byte(`<applet code="com.hp.ilo2.intgapp.intgapp.class" archive="intgapp3_231.jar"></applet>`)
	if got, want := hpJavaIRCJarFromHTML(body), "/html/intgapp3_231.jar"; got != want {
		t.Fatalf("hpJavaIRCJarFromHTML = %q, want %q", got, want)
	}
}
