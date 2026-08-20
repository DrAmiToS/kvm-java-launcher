package java

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed OpenJDK8U-jre_x64_windows_hotspot.zip
var runtimeFS embed.FS

const runtimeZip = "OpenJDK8U-jre_x64_windows_hotspot.zip"

func CacheRoot() (string, error) {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "KVMJavaLauncher"), nil
		}
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kvm-java-launcher"), nil
}

func EnsureJava() (string, error) {
	blob, err := runtimeFS.ReadFile(runtimeZip)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(blob)
	version := hex.EncodeToString(sum[:8])
	root, err := CacheRoot()
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, "java8-"+version)
	javaExe := filepath.Join(target, "bin", "javaw.exe")
	if _, err := os.Stat(javaExe); err == nil {
		return javaExe, nil
	}
	if err := os.RemoveAll(target); err != nil {
		return "", err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	if err := unzipJRE(blob, target); err != nil {
		return "", err
	}
	if _, err := os.Stat(javaExe); err == nil {
		return javaExe, nil
	}
	return "", errors.New("embedded Java was extracted, but bin/javaw.exe was not found")
}

func unzipJRE(blob []byte, target string) error {
	reader, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return err
	}
	prefix := commonPrefix(reader.File)
	for _, f := range reader.File {
		name := strings.TrimPrefix(f.Name, prefix)
		name = strings.TrimLeft(name, "/\\")
		if name == "" {
			continue
		}
		dst := filepath.Join(target, filepath.FromSlash(name))
		if !strings.HasPrefix(dst, filepath.Clean(target)+string(os.PathSeparator)) {
			return errors.New("zip contains an unsafe path")
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
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
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

func commonPrefix(files []*zip.File) string {
	for _, f := range files {
		parts := strings.Split(f.Name, "/")
		if len(parts) > 1 && parts[0] != "" {
			return parts[0] + "/"
		}
	}
	return ""
}
