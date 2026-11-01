package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	runtimedebug "runtime/debug"
	"strings"
	"time"

	"kvm-java-launcher/internal/bmc"
	"kvm-java-launcher/internal/gui"
	"kvm-java-launcher/internal/java"
	"kvm-java-launcher/internal/jnlp"
)

type options struct {
	target     string
	user       string
	password   string
	profile    string
	jnlpPath   string
	hostHeader string
	smCertMode string
	smTimeMode string
	noPause    bool
	debug      bool
}

var debugOutputEnabled bool

func main() {
	normalizeDebugArg(os.Args[1:])
	closeDebugConsole := setupDebugConsole(debugRequested(os.Args[1:]))
	defer closeDebugConsole()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "PANIC: %v\n\n%s\n", r, runtimedebug.Stack())
			os.Exit(2)
		}
	}()

	if len(os.Args) == 1 {
		configureSoftwareOpenGL(false)
		if err := guiPreflight(); err != nil {
			runInteractiveFallback(err)
			return
		}
		if err := gui.Run(); err != nil {
			fmt.Println("ERROR:", err)
			fmt.Println()
			fmt.Println("Press Enter to exit...")
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
			os.Exit(1)
		}
		return
	}

	var opts options
	flag.StringVar(&opts.target, "target", "", "BMC URL or host/IP")
	flag.StringVar(&opts.user, "user", "", "BMC username")
	flag.StringVar(&opts.password, "password", "", "BMC password")
	flag.StringVar(&opts.profile, "profile", "auto", "auto, ami, supermicro, supermicro-sol, dell, hp, lenovo, generic, jnlp, manual")
	flag.StringVar(&opts.jnlpPath, "jnlp", "", "local JNLP file")
	flag.StringVar(&opts.hostHeader, "host-header", "", "HTTP Host/SNI override for NAT, for example 192.168.7.70")
	flag.StringVar(&opts.smCertMode, "supermicro-cert-mode", "noverify", "Supermicro expired ATEN cert mode: noverify, renew-client, direct, off")
	flag.StringVar(&opts.smTimeMode, "supermicro-time-workaround", "off", "Supermicro expired ATEN cert BMC time workaround: off, auto")
	flag.BoolVar(&opts.noPause, "no-pause", false, "do not wait for Enter after an error")
	flag.BoolVar(&opts.debug, "debug", false, "write startup/debug output to console")
	flag.Parse()

	if opts.debug && opts.target == "" && opts.jnlpPath == "" && flag.NArg() == 0 {
		configureSoftwareOpenGL(true)
		if err := guiPreflight(); err != nil {
			runInteractiveFallback(err)
			return
		}
		if err := gui.Run(); err != nil {
			fmt.Println("ERROR:", err)
			os.Exit(1)
		}
		return
	}

	if opts.target == "" && opts.jnlpPath == "" {
		opts = askOptions(opts)
	}
	if err := run(opts); err != nil {
		fmt.Println("ERROR:", err)
		if !opts.noPause {
			fmt.Println()
			fmt.Println("Press Enter to exit...")
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		}
		os.Exit(1)
	}
}

func runInteractiveFallback(reason error) {
	cleanup := attachDebugConsole()
	defer cleanup()
	log.SetOutput(os.Stderr)
	fmt.Println("KVM Java Launcher")
	fmt.Println()
	fmt.Println("GUI is unavailable: OpenGL/WGL could not be initialized.")
	fmt.Println("Reason:", reason)
	fmt.Println()
	fmt.Println("You can continue in interactive CLI mode.")
	fmt.Println("For GUI over RDP/VM/server sessions, use the software OpenGL package:")
	fmt.Println("  KVM-Java-Launcher-windows-software-opengl.zip")
	fmt.Println("Mesa3D software OpenGL source:")
	fmt.Println("  https://github.com/pal1000/mesa-dist-win")
	fmt.Println()
	opts := askOptions(options{})
	if err := run(opts); err != nil {
		fmt.Println("ERROR:", err)
		fmt.Println()
		fmt.Println("Press Enter to exit...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	}
}

func normalizeDebugArg(args []string) {
	for i, arg := range args {
		if arg == "/debug" {
			args[i] = "-debug"
		}
	}
}

func debugRequested(args []string) bool {
	if os.Getenv("KVMJL_DEBUG") != "" || os.Getenv("KVM_JAVA_LAUNCHER_DEBUG") != "" {
		return true
	}
	for _, arg := range args {
		if arg == "-debug" || arg == "--debug" || arg == "/debug" {
			return true
		}
	}
	return false
}

func setupDebugConsole(enabled bool) func() {
	if !enabled {
		return func() {}
	}
	debugOutputEnabled = true
	cleanup := attachDebugConsole()
	log.SetOutput(os.Stderr)
	fmt.Println("KVM Java Launcher debug console")
	fmt.Println("Started:", time.Now().Format(time.RFC3339))
	fmt.Println("Executable:", os.Args[0])
	fmt.Println("Args:", redact(os.Args[1:]))
	fmt.Println("OS:", runtime.GOOS, runtime.GOARCH)
	fmt.Println("Session:", os.Getenv("SESSIONNAME"))
	if note := debugConsoleNote(); note != "" {
		fmt.Println(note)
	}
	fmt.Println("Note: GUI startup requires a working OpenGL 2.1 compatible Windows driver.")
	fmt.Println()
	return func() {
		fmt.Println()
		fmt.Println("Finished:", time.Now().Format(time.RFC3339))
		cleanup()
	}
}

func askOptions(opts options) options {
	in := bufio.NewReader(os.Stdin)
	fmt.Println("KVM Java Launcher")
	fmt.Println()
	opts.target = ask(in, "BMC IP/URL", opts.target)
	opts.hostHeader = ask(in, "Host header for NAT (optional)", opts.hostHeader)
	opts.user = ask(in, "User", opts.user)
	fmt.Print("Password: ")
	pw, _ := in.ReadString('\n')
	opts.password = strings.TrimSpace(pw)
	profile := ask(in, "Profile [auto/ami/supermicro/supermicro-sol/dell/hp/lenovo/generic/jnlp/manual]", "auto")
	opts.profile = strings.ToLower(profile)
	if opts.profile == "manual" || opts.profile == "jnlp" {
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

	if opts.jnlpPath != "" && (opts.profile == "" || opts.profile == "auto") {
		opts.profile = "jnlp"
	}
	if opts.profile == "manual" || opts.profile == "jnlp" {
		var httpClient *http.Client
		if !strings.EqualFold(filepath.Ext(opts.jnlpPath), ".jnlp") {
			return fmt.Errorf("local launch currently expects a .jnlp file, got %q", opts.jnlpPath)
		}
		if opts.target != "" {
			client, err := bmc.New(opts.target, opts.user, opts.password)
			if err != nil {
				return err
			}
			if opts.hostHeader != "" {
				client.SetHostHeader(opts.hostHeader)
			}
			httpClient = client.HTTP
		}
		return launchLocalJNLP(javaExe, opts.jnlpPath, httpClient)
	}

	client, err := bmc.New(opts.target, opts.user, opts.password)
	if err != nil {
		return err
	}
	if opts.hostHeader != "" {
		client.SetHostHeader(opts.hostHeader)
		fmt.Println("Using Host header override:", opts.hostHeader)
	}
	profile := bmc.Profile(strings.ToLower(opts.profile))
	profile = normalizeProfile(profile)

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
	case bmc.ProfileDell:
		fmt.Println("Looking for Dell iDRAC JNLP...")
		plan, err = bmc.PlanDell(client)
	case bmc.ProfileHP:
		fmt.Println("Looking for HPE iLO JNLP...")
		plan, err = bmc.PlanHP(client)
	case bmc.ProfileLenovo:
		fmt.Println("Looking for Lenovo remote console JNLP...")
		plan, err = bmc.PlanLenovo(client)
	case bmc.ProfileGenericJNLP:
		fmt.Println("Looking for vendor JNLP...")
		plan, err = bmc.PlanGenericJNLP(client, profile)
	case bmc.ProfileAuto:
		plan, err = planAuto(client)
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
		return launchDownloadedJNLP(javaExe, client, plan, path, work, opts.smCertMode, opts.smTimeMode)
	}
	return fmt.Errorf("profile %s did not produce a launch plan", plan.Profile)
}

func planAuto(client *bmc.Client) (*bmc.LaunchPlan, error) {
	detected, path, detectedClient := detectAutoClient(client)
	if detected != bmc.ProfileGenericJNLP {
		if detectedClient != client {
			*client = *detectedClient
		}
		if path != "" {
			fmt.Println("Detected profile:", detected, "via", client.BaseURL.Scheme+"://"+client.BaseURL.Host+path)
		} else {
			fmt.Println("Detected profile:", detected)
		}
		return planDetected(client, detected)
	}
	fmt.Println("Detected profile: generic")
	fmt.Println("Looking for vendor JNLP...")
	plan, err := bmc.PlanGenericJNLP(client, bmc.ProfileGenericJNLP)
	if err == nil {
		return plan, nil
	}
	fmt.Println("Auto fallback: trying Supermicro JNLP discovery...")
	if supermicroPlan, supermicroErr := bmc.PlanSupermicro(client); supermicroErr == nil {
		return supermicroPlan, nil
	}
	return nil, err
}

func detectAutoClient(client *bmc.Client) (bmc.Profile, string, *bmc.Client) {
	candidates := []*bmc.Client{client}
	altScheme := "http"
	if client.BaseURL.Scheme == "http" {
		altScheme = "https"
	}
	candidates = append(candidates, client.WithScheme(altScheme))
	for _, candidate := range candidates {
		if profile, path, ok := bmc.Fingerprint(candidate); ok {
			return profile, path, candidate
		}
	}
	return bmc.ProfileGenericJNLP, "", client
}

func planDetected(client *bmc.Client, profile bmc.Profile) (*bmc.LaunchPlan, error) {
	switch profile {
	case bmc.ProfileAMIMegaRAC:
		fmt.Println("Logging in as AMI MegaRAC...")
		return bmc.PlanAMI(client)
	case bmc.ProfileSupermicro:
		fmt.Println("Looking for Supermicro JNLP...")
		return bmc.PlanSupermicro(client)
	case bmc.ProfileDell:
		fmt.Println("Looking for Dell iDRAC JNLP...")
		return bmc.PlanDell(client)
	case bmc.ProfileHP:
		fmt.Println("Looking for HPE iLO JNLP...")
		return bmc.PlanHP(client)
	case bmc.ProfileLenovo:
		fmt.Println("Looking for Lenovo remote console JNLP...")
		return bmc.PlanLenovo(client)
	case bmc.ProfileGenericJNLP:
		fmt.Println("Looking for vendor JNLP...")
		return bmc.PlanGenericJNLP(client, profile)
	default:
		return nil, fmt.Errorf("unsupported profile %q", profile)
	}
}

func normalizeProfile(profile bmc.Profile) bmc.Profile {
	switch profile {
	case "jnlp":
		return bmc.ProfileLocalJNLP
	case bmc.ProfileManualJNLP:
		return bmc.ProfileLocalJNLP
	case "hpe":
		return bmc.ProfileHP
	case "ibm":
		return bmc.ProfileLenovo
	case "generic":
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

func launchLocalJNLP(javaExe, path string, httpClient *http.Client) error {
	if path == "" {
		return fmt.Errorf("JNLP path is required")
	}
	dir := filepath.Dir(path)
	parsed, err := jnlp.Parse(path, "file:///"+filepath.ToSlash(dir)+"/")
	if err != nil {
		return err
	}
	logLocalJNLPDiagnostics(parsed)
	jars, err := jnlp.DownloadResources(httpClient, parsed, dir)
	if err != nil {
		return err
	}
	return launchParsedJNLP(javaExe, nil, dir, parsed, jars, "noverify", "")
}

func logLocalJNLPDiagnostics(parsed *jnlp.File) {
	fmt.Println("Local JNLP codebase:", parsed.Codebase)
	if parsed.Applet {
		fmt.Println("Local JNLP type: applet")
	} else {
		fmt.Println("Local JNLP type: application")
	}
	fmt.Println("Local JNLP main:", parsed.Main)
	fmt.Println("Local JNLP resources:", strings.Join(parsed.Jars, ", "))
	if len(parsed.Args) > 0 {
		fmt.Println("Local JNLP arguments:", strings.Join(redactSupermicroArgs(parsed.Args), ", "))
	}
}

func launchDownloadedJNLP(javaExe string, client *bmc.Client, plan *bmc.LaunchPlan, path, dir string, supermicroCertMode, supermicroTimeMode string) error {
	base := resourceBase(client, plan.JNLPURL)
	parsed, err := jnlp.Parse(path, base)
	if err != nil {
		return fmt.Errorf("could not parse downloaded JNLP %s: %w", path, err)
	}
	fmt.Println("JNLP codebase:", parsed.Codebase)
	fmt.Println("JNLP resources:", strings.Join(parsed.Jars, ", "))
	if plan.Profile.IsSupermicro() {
		logSupermicroJNLPDiagnostics(parsed)
	}
	jars, err := jnlp.DownloadResourcesWithHeaders(client.HTTP, parsed, dir, plan.Headers)
	if err != nil {
		return err
	}
	if !plan.Profile.IsSupermicro() {
		supermicroCertMode = ""
		supermicroTimeMode = ""
	}
	return launchParsedJNLPAndWait(javaExe, client, dir, parsed, jars, supermicroCertMode, supermicroTimeMode, func() {
		fmt.Println("Closing BMC web session...")
		if err := bmc.Logout(client, plan.Profile, plan.PostExitPath); err != nil {
			fmt.Println("BMC web logout was not confirmed:", err)
		}
	})
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

func logSupermicroJNLPDiagnostics(parsed *jnlp.File) {
	if parsed.Main != "" {
		fmt.Println("JNLP main:", parsed.Main)
	}
	if len(parsed.Args) == 0 {
		return
	}
	fmt.Println("JNLP arguments:", strings.Join(redactSupermicroArgs(parsed.Args), ", "))
	if strings.Contains(strings.ToLower(parsed.Main), "kvmmain") {
		logSupermicroPorts(parsed)
	}
}

func redactSupermicroArgs(args []string) []string {
	out := append([]string(nil), args...)
	if len(out) >= 3 && looksLikeHost(out[0]) {
		out[1] = "********"
		out[2] = "********"
		return out
	}
	if len(out) >= 13 && strings.Contains(strings.ToLower(strings.Join(out[:minInt(len(out), 10)], " ")), "ikvm") {
		out[11] = "********"
		out[12] = "********"
		return out
	}
	for i := range out {
		value := strings.ToLower(out[i])
		if len(out[i]) >= 16 && !strings.HasPrefix(value, "http") && !strings.Contains(value, ".jar") {
			out[i] = "********"
		}
	}
	return out
}

func logSupermicroPorts(parsed *jnlp.File) {
	args := parsed.Args
	if len(args) >= 11 && looksLikeHost(args[0]) {
		host := args[0]
		fmt.Println("Supermicro iKVM target:", host)
		fmt.Println("Supermicro iKVM local tunnel ports:", args[4]+"/"+args[5], "(not checked remotely)")
		fmt.Println("Supermicro iKVM remote ports:", args[9]+"/"+args[10])
		checkSupermicroPort(host, args[9])
		checkSupermicroPort(host, args[10])
		logSupermicroCertificateWarning(parsed)
		return
	}
	if len(args) >= 22 {
		host := args[10]
		fmt.Println("Supermicro iKVM target:", host)
		fmt.Println("Supermicro iKVM local tunnel ports:", args[14]+"/"+args[15], "(not checked remotely)")
		fmt.Println("Supermicro iKVM TLS:", args[18])
		fmt.Println("Supermicro iKVM remote ports:", args[19]+"/"+args[20])
		checkSupermicroPort(host, args[19])
		checkSupermicroPort(host, args[20])
		logSupermicroCertificateWarning(parsed)
	}
}

func logSupermicroCertificateWarning(parsed *jnlp.File) {
	joined := strings.ToLower(strings.Join(parsed.Jars, " "))
	if strings.Contains(joined, "ikvm__v1.69.38") || strings.Contains(joined, "ikvm__v1.69.39") {
		fmt.Println("Supermicro iKVM warning: ATEN Java iKVM 1.69.38/1.69.39 may fail after 2026-05-17 because its bundled client certificate expired.")
		fmt.Println("Supermicro iKVM warning: if TCP 5900 is reachable but Java prints Retry=1..10, update BMC firmware, use HTML5 KVM, or correct/test BMC date.")
	}
}

func checkSupermicroPort(host, port string) {
	if host == "" || port == "" || port == "0" || port == "null" {
		return
	}
	addr := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", addr, 1200*time.Millisecond)
	if err != nil {
		fmt.Println("Supermicro iKVM TCP check:", addr, "not reachable:", err)
		return
	}
	_ = conn.Close()
	fmt.Println("Supermicro iKVM TCP check:", addr, "reachable")
}

func looksLikeHost(value string) bool {
	if value == "" || value == "null" {
		return false
	}
	return net.ParseIP(value) != nil || strings.Contains(value, ".")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func pathDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "/"
	}
	return p[:i+1]
}

func launchParsedJNLP(javaExe string, client *bmc.Client, dir string, parsed *jnlp.File, jars []string, supermicroCertMode, supermicroTimeMode string) error {
	return launchParsedJNLPWithStart(javaExe, client, dir, parsed, jars, supermicroCertMode, supermicroTimeMode, nil, start)
}

func launchParsedJNLPAndWait(javaExe string, client *bmc.Client, dir string, parsed *jnlp.File, jars []string, supermicroCertMode, supermicroTimeMode string, afterExit func()) error {
	return launchParsedJNLPWithStart(javaExe, client, dir, parsed, jars, supermicroCertMode, supermicroTimeMode, afterExit, start)
}

func launchParsedJNLPWithStart(javaExe string, client *bmc.Client, dir string, parsed *jnlp.File, jars []string, supermicroCertMode, supermicroTimeMode string, afterExit func(), starter func(string, []string, string) error) error {
	jars, err := unpackPackedJars(javaExe, jars)
	if err != nil {
		return err
	}
	supermicroRun := supermicroCertMode != ""
	atenDirect := false
	expiredATENCert := supermicroRun && jnlp.HasExpiredATENStunnelCertificate(jars, time.Now())
	var restore bmc.TimeRestoreFunc
	restoreDone := false
	defer func() {
		if restore != nil && !restoreDone {
			restore()
		}
	}()
	if expiredATENCert && supermicroTimeMode == "auto" {
		if client == nil {
			return fmt.Errorf("-supermicro-time-workaround auto requires a BMC target")
		}
		restore, err = bmc.BeginSupermicroTimeWorkaround(client)
		if err != nil {
			return err
		}
	} else if supermicroTimeMode != "" && supermicroTimeMode != "off" {
		return fmt.Errorf("unsupported -supermicro-time-workaround %q", supermicroTimeMode)
	}
	if supermicroRun && supermicroCertMode != "off" {
		now := time.Now()
		if expiredATENCert {
			switch supermicroCertMode {
			case "direct":
				atenDirect = bypassSupermicroStunnel(parsed)
			case "renew-client":
				jars, err = jnlp.PatchExpiredATENStunnelJars(jars, now)
				if err != nil {
					return err
				}
				jars, _, err = jnlp.PatchATENStunnelVerifyAndLogging(jars)
				if err != nil {
					return err
				}
				fmt.Println("Supermicro iKVM workaround: renewed ATEN client certificate for this downloaded session jar.")
				fmt.Println("Supermicro iKVM workaround: disabled stunnel server-certificate verification for this downloaded session jar.")
				fmt.Println("Supermicro iKVM diagnostic: enabled stunnel.log for this downloaded session jar.")
			case "noverify", "":
				var changed bool
				jars, changed, err = jnlp.PatchATENStunnelVerifyAndLogging(jars)
				if err != nil {
					return err
				}
				if changed {
					fmt.Println("Supermicro iKVM workaround: disabled stunnel server-certificate verification for this downloaded session jar.")
					fmt.Println("Supermicro iKVM diagnostic: enabled stunnel.log for this downloaded session jar.")
				}
			default:
				return fmt.Errorf("unsupported -supermicro-cert-mode %q", supermicroCertMode)
			}
		} else {
			jars, err = jnlp.PatchExpiredATENStunnelJars(jars, now)
			if err != nil {
				return err
			}
		}
	}
	native := jnlp.NativeDir(dir, jars)
	if parsed.Applet {
		runner, err := java.EnsureAppletRunner()
		if err != nil {
			return err
		}
		documentBase := parsed.DocumentBase
		if documentBase == "" {
			documentBase = parsed.Codebase
		}
		javaProps := []string{
			"-Duser.language=en",
			"-Duser.country=US",
		}
		if isHPEApplet(parsed) {
			hpProps, err := hpLegacyJavaProperties(dir)
			if err != nil {
				fmt.Println("HPE iLO compatibility: could not enable legacy Java TLS settings:", err)
			} else {
				fmt.Println("HPE iLO compatibility: enabling Java TLSv1/legacy handshake settings")
				javaProps = append(javaProps, hpProps...)
			}
		}
		args := append(javaProps, []string{
			"-Djava.library.path=" + native,
			"-cp", jnlp.Classpath(append([]string{runner}, jars...)),
			"kvm.launcher.AppletRunner",
			parsed.Main,
			documentBase,
		}...)
		for _, param := range parsed.Params {
			args = append(args, param.Name+"="+param.Value)
		}
		err = starter(javaExe, args, dir)
		if restore != nil {
			restore()
			restoreDone = true
		}
		if afterExit != nil {
			afterExit()
		}
		return err
	}
	cpJars := jars
	mainClass := parsed.Main
	kvmArgs := parsed.Args
	if atenDirect {
		runner, err := java.EnsureAppletRunner()
		if err != nil {
			return err
		}
		cpJars = append([]string{runner}, jars...)
		mainClass = "kvm.launcher.ATENDirectLauncher"
		kvmArgs = append([]string{parsed.Main}, parsed.Args...)
	}
	args := []string{
		"-Djava.library.path=" + native,
	}
	if supermicroRun {
		args = append(args, "-Djava.io.tmpdir="+dir)
	}
	args = append(args, []string{
		"-cp", jnlp.Classpath(cpJars),
		mainClass,
	}...)
	args = append(args, kvmArgs...)
	javaStart := time.Now()
	err = starter(javaExe, args, dir)
	if supermicroRun {
		printStunnelLogs(dir, javaStart.Add(-2*time.Second))
	}
	if restore != nil {
		restore()
		restoreDone = true
	}
	if afterExit != nil {
		afterExit()
	}
	return err
}

func isHPEApplet(parsed *jnlp.File) bool {
	if parsed == nil || !parsed.Applet {
		return false
	}
	mainClass := strings.ToLower(parsed.Main)
	documentBase := strings.ToLower(parsed.DocumentBase)
	return strings.Contains(mainClass, "com.hp.ilo") ||
		strings.Contains(mainClass, "intgapp") ||
		strings.Contains(documentBase, "/html/java_irc")
}

func hpLegacyJavaProperties(dir string) ([]string, error) {
	securityPath := filepath.Join(dir, "hp-legacy-java.security")
	security := strings.Join([]string{
		"# KVM Java Launcher per-session compatibility settings for legacy HPE iLO Java IRC.",
		"# Newer Java 8 builds disable TLSv1 globally, but old iLO 2/3 firmware may still require it.",
		"jdk.tls.disabledAlgorithms=SSLv3, RC4, DES, MD5withRSA, DH keySize < 1024, EC keySize < 224, anon, NULL",
		"",
	}, "\n")
	if err := os.WriteFile(securityPath, []byte(security), 0600); err != nil {
		return nil, err
	}
	return []string{
		"-Djava.security.properties=" + securityPath,
		"-Dhttps.protocols=TLSv1",
		"-Djdk.tls.client.protocols=TLSv1",
		"-Dsun.security.ssl.allowUnsafeRenegotiation=true",
		"-Dsun.security.ssl.allowLegacyHelloMessages=true",
		"-Djsse.enableSNIExtension=false",
	}, nil
}

func bypassSupermicroStunnel(parsed *jnlp.File) bool {
	args := parsed.Args
	if len(args) < 11 || !looksLikeHost(args[0]) || args[8] != "1" {
		return false
	}
	if args[9] == "" || args[9] == "0" || args[9] == "null" || args[10] == "" || args[10] == "0" || args[10] == "null" {
		return false
	}
	fmt.Println("Supermicro iKVM workaround: expired ATEN stunnel client certificate found; trying direct KVM ports instead of stunnel.")
	fmt.Println("Supermicro iKVM direct ports:", args[9]+"/"+args[10])
	direct := append([]string(nil), args...)
	direct[4] = args[9]
	direct[5] = args[10]
	if direct[6] == "0" {
		direct[6] = "2"
	}
	direct[8] = "0"
	parsed.Args = direct
	fmt.Println("Supermicro iKVM direct arguments:", strings.Join(redactSupermicroArgs(parsed.Args), ", "))
	return true
}

func printStunnelLogs(dir string, since time.Time) {
	var logs []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), "stunnel.log") {
			if info, statErr := d.Info(); statErr == nil && !since.IsZero() && info.ModTime().Before(since) {
				return nil
			}
			logs = append(logs, path)
		}
		return nil
	})
	for _, path := range logs {
		blob, err := os.ReadFile(path)
		if err != nil || len(blob) == 0 {
			continue
		}
		fmt.Println("Supermicro iKVM stunnel log:", path)
		printStunnelDiagnosis(string(blob))
		for _, line := range tailLines(string(blob), 40) {
			fmt.Println("  " + line)
		}
	}
}

func printStunnelDiagnosis(text string) {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "tlsv1 alert unknown ca") || strings.Contains(lower, "fatal: unknown ca"):
		fmt.Println("Supermicro iKVM diagnosis: BMC rejected the renewed ATEN client certificate (unknown CA).")
		fmt.Println("Supermicro iKVM diagnosis: this firmware likely requires the original embedded certificate/CA, so jar-side certificate renewal is not enough.")
	case strings.Contains(lower, "sslv3 alert certificate expired") || strings.Contains(lower, "fatal: certificate expired"):
		fmt.Println("Supermicro iKVM diagnosis: BMC rejected the original ATEN client certificate as expired.")
		fmt.Println("Supermicro iKVM diagnosis: use HTML5 KVM, update BMC firmware, or temporarily set BMC date before 2026-05-17.")
	case strings.Contains(lower, "ssl3_get_server_certificate:certificate verify failed") || strings.Contains(lower, "verification error: certificate has expired"):
		fmt.Println("Supermicro iKVM diagnosis: local stunnel rejected the expired server certificate; retry with -supermicro-cert-mode noverify.")
	}
}

func tailLines(text string, maxLines int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return lines
	}
	return lines[len(lines)-maxLines:]
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
		runner, err := java.EnsureAppletRunner()
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(javaExe, "-cp", runner, "kvm.launcher.Pack200Unpacker", path, jarPath)
		cmd.SysProcAttr = hiddenConsoleSysProcAttr()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("Pack200 unpack failed for %s: %w", path, err)
		}
		out = append(out, jarPath)
	}
	return out, nil
}

func start(javaExe string, args []string, dir string) error {
	return startAndWait(javaExe, args, dir, nil)
}

func startAndWait(javaExe string, args []string, dir string, afterExit func()) error {
	cmd := javaCommand(javaExe, args, dir)
	fmt.Println("Starting Java KVM...")
	err := cmd.Start()
	if err != nil {
		return err
	}
	if afterExit != nil {
		fmt.Println("Waiting for Java KVM to exit before closing BMC web session...")
	} else {
		fmt.Println("Waiting for Java KVM to exit...")
	}
	waitErr := cmd.Wait()
	if afterExit != nil {
		afterExit()
	}
	return waitErr
}

func javaCommand(javaExe string, args []string, dir string) *exec.Cmd {
	if runtime.GOOS != "windows" || debugOutputEnabled {
		fmt.Println("Java:", javaExe)
		fmt.Println("Workdir:", dir)
		fmt.Println("Args:", redact(args))
	}
	cmd := exec.Command(javaExe, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.SysProcAttr = hiddenConsoleSysProcAttr()
	return cmd
}

func redact(args []string) []string {
	out := append([]string(nil), args...)
	for i := 0; i < len(out); i++ {
		lower := strings.ToLower(out[i])
		if strings.HasPrefix(lower, "-password=") || strings.HasPrefix(lower, "--password=") {
			out[i] = out[i][:strings.Index(out[i], "=")+1] + "********"
			continue
		}
		if i+1 < len(out) && (out[i] == "-p" || strings.Contains(lower, "password")) {
			out[i+1] = "********"
		}
	}
	return out
}
