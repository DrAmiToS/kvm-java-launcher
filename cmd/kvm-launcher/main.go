package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"kvm-java-launcher/internal/bmc"
	"kvm-java-launcher/internal/java"
	"kvm-java-launcher/internal/jnlp"
)

type options struct {
	target   string
	user     string
	password string
	profile  string
	jnlpPath string
}

func main() {
	var opts options
	flag.StringVar(&opts.target, "target", "", "BMC URL or host/IP")
	flag.StringVar(&opts.user, "user", "", "BMC username")
	flag.StringVar(&opts.password, "password", "", "BMC password")
	flag.StringVar(&opts.profile, "profile", "auto", "auto, ami, supermicro, supermicro-sol, dell, hp, lenovo, generic, manual")
	flag.StringVar(&opts.jnlpPath, "jnlp", "", "local JNLP file for manual mode")
	flag.Parse()

	if opts.target == "" && opts.jnlpPath == "" {
		opts = askOptions(opts)
	}
	if err := run(opts); err != nil {
		fmt.Println("ERROR:", err)
		fmt.Println()
		fmt.Println("Press Enter to exit...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	}
}

func askOptions(opts options) options {
	in := bufio.NewReader(os.Stdin)
	fmt.Println("KVM Java Launcher")
	fmt.Println()
	opts.target = ask(in, "BMC IP/URL", opts.target)
	opts.user = ask(in, "User", opts.user)
	fmt.Print("Password: ")
	pw, _ := in.ReadString('\n')
	opts.password = strings.TrimSpace(pw)
	profile := ask(in, "Profile [auto/ami/supermicro/supermicro-sol/dell/hp/lenovo/generic/manual]", "auto")
	opts.profile = strings.ToLower(profile)
	if opts.profile == "manual" {
		opts.jnlpPath = ask(in, "Local JNLP path", opts.jnlpPath)
	}
	return opts
}

func ask(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	value, _ := in.ReadString('\n')
	value = strings.TrimSpace(value)
	if value == "" {
		return def
	}
	return value
}

func run(opts options) error {
	fmt.Println("Preparing embedded Java 8...")
	javaExe, err := java.EnsureJava()
	if err != nil {
		return err
	}
	root, err := java.CacheRoot()
	if err != nil {
		return err
	}

	if opts.profile == "manual" {
		return launchLocalJNLP(javaExe, opts.jnlpPath)
	}

	client, err := bmc.New(opts.target, opts.user, opts.password)
	if err != nil {
		return err
	}
	profile := bmc.Profile(strings.ToLower(opts.profile))
	profile = normalizeProfile(profile)
	if profile == bmc.ProfileAuto {
		profile = bmc.Detect(client)
		fmt.Println("Detected profile:", profile)
	}

	var plan *bmc.LaunchPlan
	switch profile {
	case bmc.ProfileAMIMegaRAC:
		fmt.Println("Logging in as AMI MegaRAC...")
		plan, err = bmc.PlanAMI(client)
	case bmc.ProfileSupermicro:
		fmt.Println("Looking for Supermicro JNLP...")
		plan, err = bmc.PlanSupermicro(client)
	case bmc.ProfileSupermicroSOL:
		fmt.Println("Looking for Supermicro SOL JNLP...")
		plan, err = bmc.PlanSupermicroSOL(client)
	case bmc.ProfileGenericJNLP:
		fmt.Println("Looking for vendor JNLP...")
		plan, err = bmc.PlanGenericJNLP(client, profile)
	default:
		err = fmt.Errorf("unsupported profile %q", profile)
	}
	if err != nil {
		return err
	}

	work, err := jnlp.WorkDir(root, client.BaseURL.Host)
	if err != nil {
		return err
	}
	if plan.Profile == bmc.ProfileAMIMegaRAC && len(plan.Jars) > 0 {
		return launchAMI(javaExe, client, work, plan)
	}
	if plan.JNLPURL != "" {
		var path string
		if len(plan.JNLPBody) > 0 {
			fmt.Println("Using JNLP:", plan.JNLPURL)
			path, err = jnlp.Save(plan.JNLPURL, plan.JNLPBody, work)
			if err != nil {
				return err
			}
		} else {
			fmt.Println("Downloading JNLP:", plan.JNLPURL)
			path, err = jnlp.DownloadWithHeaders(client.HTTP, plan.JNLPURL, work, plan.Headers)
			if err != nil {
				return err
			}
		}
		return launchDownloadedJNLP(javaExe, client, plan.JNLPURL, path, work, plan.Headers)
	}
	return fmt.Errorf("profile %s did not produce a launch plan", plan.Profile)
}

func normalizeProfile(profile bmc.Profile) bmc.Profile {
	switch profile {
	case "dell", "hp", "hpe", "lenovo", "ibm", "generic":
		return bmc.ProfileGenericJNLP
	default:
		return profile
	}
}

func launchAMI(javaExe string, client *bmc.Client, work string, plan *bmc.LaunchPlan) error {
	for _, jarURL := range plan.Jars {
		fmt.Println("Downloading:", jarURL)
		path, err := jnlp.Download(client.HTTP, jarURL, work)
		if err != nil {
			return err
		}
		if strings.EqualFold(filepath.Base(path), "Win64.jar") {
			native := filepath.Join(work, "native")
			if err := os.MkdirAll(native, 0o755); err != nil {
				return err
			}
			if err := jnlp.ExtractZip(path, native); err != nil {
				return err
			}
		}
	}
	sep := string(os.PathListSeparator)
	cp := strings.Join([]string{filepath.Join(work, "JViewer.jar"), filepath.Join(work, "JViewer-SOC.jar")}, sep)
	native := filepath.Join(work, "native")
	args := []string{
		"-Djava.library.path=" + native,
		"-cp", cp,
		"com.ami.kvm.jviewer.JViewer",
	}
	args = append(args, normalizeAMIArgs(plan.Args)...)
	return start(javaExe, args, work)
}

func normalizeAMIArgs(args []string) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if (out[i] == "-webport" || out[i] == "-websecureport") && out[i+1] == "" {
			out[i+1] = "443"
		}
	}
	return out
}

func launchLocalJNLP(javaExe, path string) error {
	if path == "" {
		return fmt.Errorf("JNLP path is required")
	}
	dir := filepath.Dir(path)
	parsed, err := jnlp.Parse(path, "file:///"+filepath.ToSlash(dir)+"/")
	if err != nil {
		return err
	}
	jars, err := jnlp.DownloadResources(nil, parsed, dir)
	if err != nil {
		return err
	}
	return launchParsedJNLP(javaExe, dir, parsed, jars)
}

func launchDownloadedJNLP(javaExe string, client *bmc.Client, jnlpURL, path, dir string, headers http.Header) error {
	base := resourceBase(client, jnlpURL)
	parsed, err := jnlp.Parse(path, base)
	if err != nil {
		return fmt.Errorf("could not parse downloaded JNLP %s: %w", path, err)
	}
	fmt.Println("JNLP codebase:", parsed.Codebase)
	fmt.Println("JNLP resources:", strings.Join(parsed.Jars, ", "))
	jars, err := jnlp.DownloadResourcesWithHeaders(client.HTTP, parsed, dir, headers)
	if err != nil {
		return err
	}
	return launchParsedJNLP(javaExe, dir, parsed, jars)
}

func resourceBase(client *bmc.Client, jnlpURL string) string {
	u, err := url.Parse(jnlpURL)
	if err != nil {
		return client.BaseURL.ResolveReference(&url.URL{Path: "/"}).String()
	}
	if !u.IsAbs() {
		u = client.BaseURL.ResolveReference(u)
	}
	u.RawQuery = ""
	u.Fragment = ""
	if !strings.HasSuffix(u.Path, "/") {
		u.Path = pathDir(u.Path)
	}
	return u.String()
}

func pathDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "/"
	}
	return p[:i+1]
}

func launchParsedJNLP(javaExe, dir string, parsed *jnlp.File, jars []string) error {
	jars, err := unpackPackedJars(javaExe, jars)
	if err != nil {
		return err
	}
	native := jnlp.NativeDir(dir, jars)
	args := []string{
		"-Djava.library.path=" + native,
		"-cp", jnlp.Classpath(jars),
		parsed.Main,
	}
	args = append(args, parsed.Args...)
	return start(javaExe, args, dir)
}

func unpackPackedJars(javaExe string, jars []string) ([]string, error) {
	out := make([]string, 0, len(jars))
	for _, path := range jars {
		if !strings.HasSuffix(strings.ToLower(path), ".pack.gz") {
			out = append(out, path)
			continue
		}
		jarPath := strings.TrimSuffix(path, ".pack.gz")
		fmt.Println("Unpacking Pack200:", filepath.Base(path))
		unpackExe := filepath.Join(filepath.Dir(javaExe), unpack200Name())
		cmd := exec.Command(unpackExe, path, jarPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("unpack200 failed for %s: %w", path, err)
		}
		out = append(out, jarPath)
	}
	return out, nil
}

func unpack200Name() string {
	if runtime.GOOS == "windows" {
		return "unpack200.exe"
	}
	return "unpack200"
}

func start(javaExe string, args []string, dir string) error {
	if runtime.GOOS != "windows" {
		fmt.Println("Java:", javaExe)
		fmt.Println("Args:", redact(args))
	}
	cmd := exec.Command(javaExe, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	fmt.Println("Starting Java KVM...")
	return cmd.Start()
}

func redact(args []string) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == "-p" || strings.Contains(strings.ToLower(out[i]), "password") {
			out[i+1] = "********"
		}
	}
	return out
}
