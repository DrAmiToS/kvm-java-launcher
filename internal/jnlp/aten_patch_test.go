package jnlp

import (
	"archive/zip"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPatchExpiredATENStunnelJar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := testCertificatePEM(t, key, time.Date(2016, 5, 19, 9, 46, 36, 0, time.UTC), time.Date(2026, 5, 17, 9, 46, 36, 0, time.UTC))
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	jarPath := filepath.Join(dir, "iKVM__V1.69.38.0x0.jar")
	writeTestJar(t, jarPath, map[string][]byte{
		"META-INF/MANIFEST.MF":   []byte("Manifest-Version: 1.0\n"),
		"META-INF/SMCCERT2.SF":   []byte("signature"),
		"META-INF/SMCCERT2.RSA":  []byte("signature"),
		"tw/com/aten/KVM.class":  []byte("class"),
		"res/client.crt":         certPEM,
		"res/client.key":         keyPEM,
		"res/server.crt":         certPEM,
		"res/win/stunnel.conf":   []byte("[ikvm]\r\nverify = 3\r\ncert = client.crt\r\n"),
		"res/linux/stunnel.conf": []byte("[ikvm]\n  verify = 3\n"),
		"res/mac/stunnel.conf":   []byte("[ikvm]\nverify = 3\n"),
		"res/win/stunnel.exe":    []byte("exe"),
	})

	patched, changed, err := patchExpiredATENStunnelJar(jarPath, time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected jar to be patched")
	}
	if patched == jarPath {
		t.Fatal("expected patched jar path")
	}

	zr, err := zip.OpenReader(patched)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	if entries["META-INF/SMCCERT2.SF"] != nil || entries["META-INF/SMCCERT2.RSA"] != nil {
		t.Fatal("signature files should be stripped from patched jar")
	}
	patchedCert, err := parsePEMCertificate(mustReadZipFile(t, entries["res/client.crt"]))
	if err != nil {
		t.Fatal(err)
	}
	if !patchedCert.NotAfter.After(time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("patched cert NotAfter = %s", patchedCert.NotAfter)
	}
	conf := string(mustReadZipFile(t, entries["res/win/stunnel.conf"]))
	if !strings.Contains(conf, "verify = 0") {
		t.Fatalf("stunnel verify was not patched: %q", conf)
	}
}

func TestPatchStunnelOutput(t *testing.T) {
	t.Parallel()
	got := string(patchStunnelOutput(patchStunnelVerify([]byte("; output = stunnel.log\nverify = 3\n"))))
	if !strings.Contains(got, "output = stunnel.log") {
		t.Fatalf("missing output line: %q", got)
	}
	if !strings.Contains(got, "verify = 0") {
		t.Fatalf("verify line was not patched: %q", got)
	}
	if strings.Contains(got, "; output") {
		t.Fatalf("output line still commented: %q", got)
	}
}

func testCertificatePEM(t *testing.T, key *rsa.PrivateKey, notBefore, notAfter time.Time) []byte {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber: x509SerialOne(),
		Subject: pkix.Name{
			Country:            []string{"US"},
			Province:           []string{"California"},
			Locality:           []string{"San Jose"},
			Organization:       []string{"Super Micro Computer"},
			OrganizationalUnit: []string{"Software"},
			CommonName:         "IPMI",
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func x509SerialOne() *big.Int {
	return big.NewInt(1)
}

func writeTestJar(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()
	for name, blob := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(blob); err != nil {
			t.Fatal(err)
		}
	}
}

func mustReadZipFile(t *testing.T, f *zip.File) []byte {
	t.Helper()
	if f == nil {
		t.Fatal("zip entry missing")
	}
	blob, err := readZipFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return blob
}
