package jnlp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseIncludesMatchingNativeLib(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "launch.jnlp")
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	body := `<?xml version="1.0"?>
<jnlp codebase="http://(null):80/Java">
  <resources>
    <jar href="release/JViewer.jar"/>
  </resources>
  <resources os="` + runtime.GOOS + `" arch="` + arch + `">
    <nativelib href="release/Native.jar"/>
  </resources>
  <application-desc main-class="com.example.Main">
    <argument>one</argument>
  </application-desc>
</jnlp>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, err := Parse(path, "https://bmc.example/Java/")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Codebase != "https://bmc.example/Java/" {
		t.Fatalf("codebase = %q", parsed.Codebase)
	}
	if len(parsed.Jars) != 2 {
		t.Fatalf("jars = %#v", parsed.Jars)
	}
	if parsed.Jars[1] != "release/Native.jar" {
		t.Fatalf("native jar missing: %#v", parsed.Jars)
	}
}

func TestUnversionedJarURL(t *testing.T) {
	t.Parallel()

	got := unversionedJarURL("http://10.168.68.120:80/iKVM__V1.69.21.0x0.jar")
	want := "http://10.168.68.120:80/iKVM.jar"
	if got != want {
		t.Fatalf("unversionedJarURL = %q, want %q", got, want)
	}
}

func TestParseAppletDesc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ilo.jnlp")
	body := `<?xml version="1.0"?>
<jnlp codebase="https://ilo.example/">
  <resources>
    <jar href="html/intgapp4_231.jar"/>
  </resources>
  <applet-desc main-class="com.hp.ilo2.intgapp.intgapp" documentbase="https://ilo.example/html/java_irc.html">
    <param name="RCINFO1" value="abcdef"/>
    <param name="INFO1" value="17988"/>
  </applet-desc>
</jnlp>`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, err := Parse(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Applet {
		t.Fatal("expected applet JNLP")
	}
	if parsed.Main != "com.hp.ilo2.intgapp.intgapp" {
		t.Fatalf("main = %q", parsed.Main)
	}
	if parsed.DocumentBase != "https://ilo.example/html/java_irc.html" {
		t.Fatalf("document base = %q", parsed.DocumentBase)
	}
	if len(parsed.Params) != 2 || parsed.Params[0].Name != "RCINFO1" || parsed.Params[0].Value != "abcdef" {
		t.Fatalf("params = %#v", parsed.Params)
	}
}

func TestResourceCandidatesForVersionedJar(t *testing.T) {
	t.Parallel()

	got := resourceCandidates("http://10.168.68.120:80/iKVM__V1.69.21.0x0.jar")
	want := []string{
		"http://10.168.68.120:80/iKVM__V1.69.21.0x0.jar",
		"http://10.168.68.120:80/iKVM__V1.69.21.0x0.jar.pack.gz",
		"http://10.168.68.120:80/iKVM.jar?version-id=1.69.21.0x0",
		"http://10.168.68.120:80/iKVM.jar",
	}
	for i, wantURL := range want {
		if len(got) <= i {
			t.Fatalf("missing candidate %d = %q; all = %#v", i, wantURL, got)
		}
		if got[i] != wantURL {
			t.Fatalf("candidate %d = %q, want %q; all = %#v", i, got[i], wantURL, got)
		}
	}
}
