package jnlp

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func WorkDir(root, host string) (string, error) {
	sum := sha1.Sum([]byte(host))
	dir := filepath.Join(root, "sessions", hex.EncodeToString(sum[:8]))
	return dir, os.MkdirAll(dir, 0o755)
}

func Download(client *http.Client, rawURL, dir string) (string, error) {
	return DownloadWithHeaders(client, rawURL, dir, nil)
}

func DownloadWithHeaders(client *http.Client, rawURL, dir string, headers http.Header) (string, error) {
	name := downloadName(rawURL)
	if name == "" {
		name = "download.bin"
	}
	target := filepath.Join(dir, name)
	if strings.HasPrefix(rawURL, "file:///") {
		src := filepath.FromSlash(strings.TrimPrefix(rawURL, "file:///"))
		if filepath.Clean(src) == filepath.Clean(target) {
			return target, nil
		}
		in, err := os.Open(src)
		if err != nil {
			return "", err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return "", err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return target, err
	}
	if client == nil {
		return "", &HTTPError{Status: "HTTP client is required for " + rawURL}
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", &HTTPError{Status: rawURL + ": " + resp.Status}
	}
	out, err := os.Create(target)
	if err != nil {
		return "", err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return target, err
}

func Save(rawURL string, blob []byte, dir string) (string, error) {
	name := downloadName(rawURL)
	if name == "" || !strings.Contains(strings.ToLower(name), ".jnlp") {
		name = "launch.jnlp"
	}
	target := filepath.Join(dir, name)
	if err := os.WriteFile(target, blob, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

func downloadName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Path != "" {
		name := filepath.Base(u.Path)
		if name != "." && name != "/" {
			return name
		}
	}
	name := rawURL[strings.LastIndex(rawURL, "/")+1:]
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	return name
}

func ExtractZip(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		dst := filepath.Join(dir, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(dst, filepath.Clean(dir)+string(os.PathSeparator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		in, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dst)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

type HTTPError struct {
	Status string
}

func (e *HTTPError) Error() string {
	return e.Status
}
