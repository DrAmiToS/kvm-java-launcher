package jnlp

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type File struct {
	Codebase string
	Jars     []string
	Main     string
	Args     []string
}

type rawJNLP struct {
	Codebase  string `xml:"codebase,attr"`
	Resources []struct {
		OS   string `xml:"os,attr"`
		Arch string `xml:"arch,attr"`
		Jars []struct {
			Href string `xml:"href,attr"`
			Main string `xml:"main,attr"`
		} `xml:"jar"`
		NativeLibs []struct {
			Href string `xml:"href,attr"`
		} `xml:"nativelib"`
	} `xml:"resources"`
	Application struct {
		Main string   `xml:"main-class,attr"`
		Args []string `xml:"argument"`
	} `xml:"application-desc"`
	Applets []struct {
		Main string `xml:"main-class,attr"`
	} `xml:"applet-desc"`
}

func Parse(path, fallbackCodebase string) (*File, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw rawJNLP
	if err := xml.Unmarshal(blob, &raw); err != nil {
		return nil, err
	}
	f := &File{Codebase: raw.Codebase, Main: raw.Application.Main, Args: raw.Application.Args}
	if f.Codebase == "" || strings.Contains(f.Codebase, "(null)") {
		f.Codebase = fallbackCodebase
	}
	if f.Main == "" && len(raw.Applets) > 0 {
		f.Main = raw.Applets[0].Main
	}
	for _, res := range raw.Resources {
		if !matchesRuntime(res.OS, res.Arch) {
			continue
		}
		for _, jar := range res.Jars {
			if jar.Href == "" {
				continue
			}
			f.Jars = appendStringUnique(f.Jars, jar.Href)
		}
		for _, jar := range res.NativeLibs {
			if jar.Href == "" {
				continue
			}
			f.Jars = appendStringUnique(f.Jars, jar.Href)
		}
	}
	return f, nil
}

func appendStringUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func matchesRuntime(osName, arch string) bool {
	osName = strings.ToLower(strings.TrimSpace(osName))
	arch = strings.ToLower(strings.TrimSpace(arch))
	if osName != "" {
		switch runtime.GOOS {
		case "windows":
			if !strings.Contains(osName, "windows") {
				return false
			}
		case "linux":
			if !strings.Contains(osName, "linux") {
				return false
			}
		case "darwin":
			if !strings.Contains(osName, "mac") && !strings.Contains(osName, "darwin") {
				return false
			}
		}
	}
	if arch == "" {
		return true
	}
	goarch := runtime.GOARCH
	switch {
	case goarch == "amd64":
		return arch == "amd64" || arch == "x86_64" || arch == "x64"
	case goarch == "386":
		return arch == "x86" || arch == "i386" || arch == "386"
	case goarch == "arm64":
		return arch == "arm64" || arch == "aarch64"
	default:
		return strings.Contains(arch, goarch)
	}
}

func DownloadResources(client *http.Client, parsed *File, dir string) ([]string, error) {
	return DownloadResourcesWithHeaders(client, parsed, dir, nil)
}

func DownloadResourcesWithHeaders(client *http.Client, parsed *File, dir string, headers http.Header) ([]string, error) {
	base, err := url.Parse(parsed.Codebase)
	if err != nil {
		return nil, err
	}
	var local []string
	for _, href := range parsed.Jars {
		ref, err := url.Parse(href)
		if err != nil {
			return nil, err
		}
		rawURL := base.ResolveReference(ref).String()
		path, err := downloadResourceCandidates(client, rawURL, dir, headers)
		if err != nil {
			return nil, err
		}
		local = append(local, path)
	}
	if parsed.Main == "" {
		parsed.Main = findMainClass(local)
	}
	if parsed.Main == "" {
		return nil, fmt.Errorf("JNLP main class was not found")
	}
	return local, nil
}

func downloadResourceCandidates(client *http.Client, rawURL, dir string, headers http.Header) (string, error) {
	var errs []string
	for i, candidate := range resourceCandidates(rawURL) {
		if i > 0 {
			fmt.Println("Retrying resource:", candidate)
		}
		path, err := DownloadWithHeaders(client, candidate, dir, headers)
		if err == nil {
			return path, nil
		}
		errs = append(errs, err.Error())
	}
	return "", fmt.Errorf("resource download failed (%s)", strings.Join(errs, "; "))
}

func resourceCandidates(rawURL string) []string {
	out := withPackedVariant(nil, rawURL)
	for _, candidate := range versionedJarProtocolURLs(rawURL) {
		out = withPackedVariant(out, candidate)
	}
	if fallbackURL := unversionedJarURL(rawURL); fallbackURL != "" {
		out = withPackedVariant(out, fallbackURL)
		for _, candidate := range alternateJarDirectoryURLs(fallbackURL) {
			out = withPackedVariant(out, candidate)
		}
	}
	return out
}

func withPackedVariant(values []string, rawURL string) []string {
	values = appendUnique(values, rawURL)
	if strings.HasSuffix(strings.ToLower(rawURL), ".jar") {
		values = appendUnique(values, rawURL+".pack.gz")
	}
	return values
}

func versionedJarProtocolURLs(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	name := filepath.Base(u.Path)
	base, version, ok := splitVersionedJarName(name)
	if !ok {
		return nil
	}
	u.Path = strings.TrimSuffix(u.Path, name) + base + ".jar"
	q := u.Query()
	q.Set("version-id", version)
	u.RawQuery = q.Encode()
	return []string{u.String()}
}

func unversionedJarURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	name := filepath.Base(u.Path)
	base, _, ok := splitVersionedJarName(name)
	if !ok {
		return ""
	}
	u.Path = strings.TrimSuffix(u.Path, name) + base + ".jar"
	return u.String()
}

func splitVersionedJarName(name string) (string, string, bool) {
	lower := strings.ToLower(name)
	versionAt := strings.Index(lower, "__v")
	if versionAt <= 0 || !strings.HasSuffix(lower, ".jar") {
		return "", "", false
	}
	version := name[versionAt+3 : len(name)-4]
	return name[:versionAt], version, true
}

func alternateJarDirectoryURLs(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	name := filepath.Base(u.Path)
	var out []string
	for _, dir := range []string{"/Java/", "/Java/release/", "/lib/", "/cgi/", "/cgi-bin/"} {
		v := *u
		v.Path = dir + name
		out = append(out, v.String())
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func findMainClass(jars []string) string {
	for _, jar := range jars {
		zr, err := zip.OpenReader(jar)
		if err != nil {
			continue
		}
		for _, f := range zr.File {
			if strings.EqualFold(f.Name, "META-INF/MANIFEST.MF") {
				main := manifestMain(f)
				zr.Close()
				if main != "" {
					return main
				}
				break
			}
		}
		zr.Close()
	}
	return ""
}

func manifestMain(f *zip.File) string {
	in, err := f.Open()
	if err != nil {
		return ""
	}
	defer in.Close()
	blob, err := io.ReadAll(in)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(blob), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "main-class:") {
			return strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
	}
	return ""
}

func Classpath(paths []string) string {
	return strings.Join(paths, string(os.PathListSeparator))
}

func NativeDir(dir string, jars []string) string {
	native := filepath.Join(dir, "native")
	for _, path := range jars {
		if isNativeArchive(path) {
			_ = os.MkdirAll(native, 0o755)
			_ = ExtractZip(path, native)
		}
	}
	return native
}

func isNativeArchive(path string) bool {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if strings.HasSuffix(name, ".dll") || strings.HasSuffix(name, ".so") || strings.HasSuffix(name, ".dylib") || strings.HasSuffix(name, ".jnilib") {
			return true
		}
	}
	return false
}
