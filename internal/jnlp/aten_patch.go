package jnlp

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const atenClientCertPath = "res/client.crt"
const atenClientKeyPath = "res/client.key"

// PatchExpiredATENStunnelJars applies the narrow Supermicro/ATEN workaround for
// iKVM jars whose bundled stunnel client certificate expired in 2026.
func PatchExpiredATENStunnelJars(jars []string, now time.Time) ([]string, error) {
	out := append([]string(nil), jars...)
	for i, jar := range out {
		patched, changed, err := patchExpiredATENStunnelJar(jar, now)
		if err != nil {
			return nil, err
		}
		if changed {
			out[i] = patched
		}
	}
	return out, nil
}

func PatchATENStunnelVerifyAndLogging(jars []string) ([]string, bool, error) {
	out := append([]string(nil), jars...)
	changedAny := false
	for i, jar := range out {
		patched, changed, err := patchATENStunnelVerifyAndLogging(jar)
		if err != nil {
			return nil, false, err
		}
		if changed {
			out[i] = patched
			changedAny = true
		}
	}
	return out, changedAny, nil
}

func patchATENStunnelVerifyAndLogging(path string) (string, bool, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".jar") {
		return path, false, nil
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return path, false, nil
	}
	defer zr.Close()

	replacements := map[string][]byte{}
	for _, f := range zr.File {
		switch f.Name {
		case "res/win/stunnel.conf", "res/linux/stunnel.conf", "res/mac/stunnel.conf":
			blob, err := readZipFile(f)
			if err != nil {
				return "", false, err
			}
			replacements[f.Name] = patchStunnelOutput(patchStunnelVerify(blob))
		}
	}
	if len(replacements) == 0 {
		return path, false, nil
	}
	patchedPath := strings.TrimSuffix(path, ".jar") + "-stunnel-noverify.jar"
	if err := rewriteJar(patchedPath, zr.File, replacements); err != nil {
		return "", false, err
	}
	return patchedPath, true, nil
}

func HasExpiredATENStunnelCertificate(jars []string, now time.Time) bool {
	for _, jar := range jars {
		expired, err := hasExpiredATENStunnelCertificate(jar, now)
		if err == nil && expired {
			return true
		}
	}
	return false
}

func hasExpiredATENStunnelCertificate(path string, now time.Time) (bool, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".jar") {
		return false, nil
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != atenClientCertPath {
			continue
		}
		certPEM, err := readZipFile(f)
		if err != nil {
			return false, err
		}
		cert, err := parsePEMCertificate(certPEM)
		if err != nil {
			return false, err
		}
		return !now.Before(cert.NotAfter), nil
	}
	return false, nil
}

func patchExpiredATENStunnelJar(path string, now time.Time) (string, bool, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".jar") {
		return path, false, nil
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return path, false, nil
	}
	defer zr.Close()

	entries := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	certEntry := entries[atenClientCertPath]
	keyEntry := entries[atenClientKeyPath]
	if certEntry == nil || keyEntry == nil {
		return path, false, nil
	}
	certPEM, err := readZipFile(certEntry)
	if err != nil {
		return "", false, err
	}
	cert, err := parsePEMCertificate(certPEM)
	if err != nil {
		return "", false, fmt.Errorf("parse %s in %s: %w", atenClientCertPath, path, err)
	}
	if now.Before(cert.NotAfter) {
		return path, false, nil
	}
	keyPEM, err := readZipFile(keyEntry)
	if err != nil {
		return "", false, err
	}
	key, err := parsePEMRSAPrivateKey(keyPEM)
	if err != nil {
		return "", false, fmt.Errorf("parse %s in %s: %w", atenClientKeyPath, path, err)
	}
	certKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return "", false, fmt.Errorf("%s public key is %T, not RSA, in %s", atenClientCertPath, cert.PublicKey, path)
	}
	if key.PublicKey.N.Cmp(certKey.N) != 0 {
		return "", false, fmt.Errorf("%s private key does not match %s in %s", atenClientKeyPath, atenClientCertPath, path)
	}

	newCertPEM, err := renewSelfSignedCertificate(cert, key, now)
	if err != nil {
		return "", false, err
	}
	replacements := map[string][]byte{
		atenClientCertPath: newCertPEM,
	}
	for _, conf := range []string{"res/win/stunnel.conf", "res/linux/stunnel.conf", "res/mac/stunnel.conf"} {
		if entry := entries[conf]; entry != nil {
			blob, err := readZipFile(entry)
			if err != nil {
				return "", false, err
			}
			replacements[conf] = patchStunnelVerify(blob)
		}
	}

	patchedPath := strings.TrimSuffix(path, ".jar") + "-patched.jar"
	if err := rewriteJar(patchedPath, zr.File, replacements); err != nil {
		return "", false, err
	}
	fmt.Printf("Supermicro iKVM patch: renewed expired ATEN client certificate in %s (%s -> %s)\n", filepath.Base(path), cert.NotAfter.Format("2006-01-02"), renewedUntil(cert, now).Format("2006-01-02"))
	fmt.Println("Supermicro iKVM patch: disabled stunnel server-certificate verification for this downloaded session jar.")
	return patchedPath, true, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func parsePEMCertificate(blob []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(blob)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("PEM certificate block was not found")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parsePEMRSAPrivateKey(blob []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(blob)
	if block == nil {
		return nil, fmt.Errorf("PEM private key block was not found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is %T, not RSA", parsed)
	}
	return key, nil
}

func renewSelfSignedCertificate(old *x509.Certificate, key *rsa.PrivateKey, now time.Time) ([]byte, error) {
	template := &x509.Certificate{
		SerialNumber:          cloneBigInt(old.SerialNumber),
		Subject:               old.Subject,
		NotBefore:             old.NotBefore,
		NotAfter:              renewedUntil(old, now),
		KeyUsage:              old.KeyUsage,
		ExtKeyUsage:           append([]x509.ExtKeyUsage(nil), old.ExtKeyUsage...),
		BasicConstraintsValid: old.BasicConstraintsValid,
		IsCA:                  old.IsCA,
		SignatureAlgorithm:    old.SignatureAlgorithm,
	}
	if template.KeyUsage == 0 {
		template.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renewedUntil(old *x509.Certificate, now time.Time) time.Time {
	extended := old.NotAfter.AddDate(10, 0, 0)
	minimum := now.AddDate(10, 0, 0)
	if extended.Before(minimum) {
		return minimum
	}
	return extended
}

func cloneBigInt(v *big.Int) *big.Int {
	if v == nil {
		return big.NewInt(1)
	}
	return new(big.Int).Set(v)
}

func patchStunnelVerify(blob []byte) []byte {
	lines := strings.Split(string(blob), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "verify") {
			prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = prefix + "verify = 0"
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func patchStunnelOutput(blob []byte) []byte {
	lines := strings.Split(string(blob), "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "output") || strings.HasPrefix(lower, "; output") {
			prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = prefix + "output = stunnel.log"
			replaced = true
			continue
		}
	}
	if !replaced {
		out := []string{"output = stunnel.log"}
		out = append(out, lines...)
		lines = out
	}
	return []byte(strings.Join(lines, "\n"))
}

func rewriteJar(path string, files []*zip.File, replacements map[string][]byte) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()
	for _, f := range files {
		if isJarSignatureFile(f.Name) {
			continue
		}
		h := f.FileHeader
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(&h)
		if err != nil {
			return err
		}
		if blob, ok := replacements[f.Name]; ok {
			if _, err := w.Write(blob); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, rc)
		closeErr := rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func isJarSignatureFile(name string) bool {
	upper := strings.ToUpper(name)
	if !strings.HasPrefix(upper, "META-INF/") {
		return false
	}
	return strings.HasSuffix(upper, ".SF") || strings.HasSuffix(upper, ".RSA") || strings.HasSuffix(upper, ".DSA") || strings.HasSuffix(upper, ".EC")
}
