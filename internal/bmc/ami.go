package bmc

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Profile string

const (
	ProfileAuto          Profile = "auto"
	ProfileAMIMegaRAC    Profile = "ami"
	ProfileSupermicro    Profile = "supermicro"
	ProfileSupermicroSOL Profile = "supermicro-sol"
	ProfileDell          Profile = "dell"
	ProfileHP            Profile = "hp"
	ProfileLenovo        Profile = "lenovo"
	ProfileGenericJNLP   Profile = "generic"
	ProfileManualJNLP    Profile = "manual"
	ProfileLocalJNLP     Profile = "jnlp"
)

type LaunchPlan struct {
	Profile      Profile
	JNLPURL      string
	JNLPBody     []byte
	Jars         []string
	Args         []string
	Headers      http.Header
	PostExitPath string
}

func (p Profile) IsSupermicro() bool {
	return p == ProfileSupermicro || p == ProfileSupermicroSOL
}

type AMISession struct {
	Cookie       string
	CSRFToken    string
	SessionToken string
	KVMPort      string
	KVMSecure    string
	WebPort      string
	OEMFeatures  string
	ExtendedPriv string
}

func Detect(c *Client) Profile {
	if profile, _, ok := Fingerprint(c); ok {
		return profile
	}
	return ProfileGenericJNLP
}

func Fingerprint(c *Client) (Profile, string, bool) {
	paths := []string{
		"/",
		"/login.html",
		"/index.html",
		"/index.htm",
		"/start.html",
		"/cgi/login.cgi",
		"/cgi/url_redirect.cgi?url_name=ikvm&url_type=jwsk",
		"/html/login.html",
		"/html/java_irc.html",
		"/redfish/v1/",
	}
	for _, path := range paths {
		body, err := c.GetWithHeaders(path, http.Header{"User-Agent": {"Mozilla/5.0"}})
		if err != nil {
			continue
		}
		if profile, ok := fingerprintBody(body); ok {
			return profile, path, true
		}
	}
	return ProfileGenericJNLP, "", false
}

func fingerprintBody(body []byte) (Profile, bool) {
	text := strings.ToLower(string(body))
	if strings.Contains(text, "megarac") || strings.Contains(text, "american megatrends") || strings.Contains(text, "jviewer.jnlp") {
		return ProfileAMIMegaRAC, true
	}
	if strings.Contains(text, "supermicro") ||
		strings.Contains(text, "aten international") ||
		strings.Contains(text, "url_redirect.cgi") ||
		strings.Contains(text, "man_ikvm") ||
		strings.Contains(text, "ikvm") && strings.Contains(text, "sid") {
		return ProfileSupermicro, true
	}
	if strings.Contains(text, "idrac") ||
		strings.Contains(text, "integrated dell remote access") ||
		strings.Contains(text, "poweredge") ||
		strings.Contains(text, "/data/login") {
		return ProfileDell, true
	}
	if strings.Contains(text, "hpe ilo") ||
		strings.Contains(text, "hp ilo") ||
		strings.Contains(text, "integrated lights-out") ||
		strings.Contains(text, "java_irc.html") ||
		strings.Contains(text, "intgapp") {
		return ProfileHP, true
	}
	if strings.Contains(text, "xclarity") ||
		strings.Contains(text, "integrated management module") ||
		strings.Contains(text, "lenovo") && strings.Contains(text, "remote console") {
		return ProfileLenovo, true
	}
	return ProfileGenericJNLP, false
}

func LoginAMI(c *Client) (*AMISession, error) {
	body, err := c.PostForm("/rpc/WEBSES/create.asp", url.Values{
		"WEBVAR_USERNAME": {c.Username},
		"WEBVAR_PASSWORD": {c.Password},
	})
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`HAPI_STATUS\s*:?\s*0`).Match(body) {
		return nil, errors.New("AMI login failed: HAPI_STATUS is not 0")
	}
	session := &AMISession{
		Cookie:    field(body, "SESSION_COOKIE"),
		CSRFToken: field(body, "CSRFTOKEN"),
		WebPort:   defaultPort(c.BaseURL.Port()),
	}
	if session.Cookie != "" && c.HTTP.Jar != nil {
		c.HTTP.Jar.SetCookies(c.BaseURL, []*http.Cookie{
			{Name: "SessionCookie", Value: session.Cookie, Path: "/"},
		})
	}
	return session, nil
}

func PlanAMI(c *Client) (*LaunchPlan, error) {
	session, err := LoginAMI(c)
	if err != nil {
		return nil, err
	}
	if err := fillAMISession(c, session); err != nil {
		return nil, err
	}
	return &LaunchPlan{
		Profile: ProfileAMIMegaRAC,
		JNLPURL: c.URL("/Java/jviewer.jnlp"),
		Jars: []string{
			c.URL("/Java/release/JViewer.jar"),
			c.URL("/Java/release/JViewer-SOC.jar"),
			c.URL("/Java/release/Win64.jar"),
		},
		Args: []string{
			"-apptype", "JViewer",
			"-title", c.BaseURL.Hostname(),
			"-hostname", c.BaseURL.Hostname(),
			"-kvmtoken", session.SessionToken,
			"-kvmtokentype", "2",
			"-kvmsecure", session.KVMSecure,
			"-kvmport", session.KVMPort,
			"-vmsecure", session.KVMSecure,
			"-cdstate", "0",
			"-cdport", "5120",
			"-cdnum", "1",
			"-fdstate", "0",
			"-fdport", "5120",
			"-fdnum", "1",
			"-hdstate", "0",
			"-hdport", "5120",
			"-hdnum", "1",
			"-webcookie", session.Cookie,
			"-websecure", "1",
			"-webport", "80",
			"-websecureport", session.WebPort,
			"-keyboardlayout", "0",
			"-singleportenabled", "0",
			"-extendedpriv", session.ExtendedPriv,
			"-oemfeatures", session.OEMFeatures,
			"-localization", "EN",
		},
	}, nil
}

func fillAMISession(c *Client, session *AMISession) error {
	if body, err := c.Get("/rpc/getsessiontoken.asp"); err == nil {
		session.SessionToken = field(body, "SESSION_TOKEN")
	}
	if body, err := c.Get("/rpc/getadvisercfg.asp"); err == nil {
		session.KVMPort = intField(body, "V_STR_KVM_PORT", "7578")
		session.KVMSecure = intField(body, "V_STR_SECURE_CHANNEL", "0")
		session.WebPort = intField(body, "V_STR_WEB_PORT", session.WebPort)
		session.OEMFeatures = intField(body, "V_STR_OEM_FEATURE_STATUS", "1")
	}
	if body, err := c.Get("/rpc/getrole.asp"); err == nil {
		session.ExtendedPriv = intField(body, "EXTENDED_PRIV", "3")
	}
	if session.SessionToken == "" {
		return errors.New("AMI session token was not returned")
	}
	if session.KVMPort == "" {
		session.KVMPort = "7578"
	}
	if session.KVMSecure == "" {
		session.KVMSecure = "0"
	}
	if session.WebPort == "" {
		session.WebPort = defaultPort(c.BaseURL.Port())
	}
	if session.ExtendedPriv == "" {
		session.ExtendedPriv = "3"
	}
	if session.OEMFeatures == "" {
		session.OEMFeatures = "1"
	}
	return nil
}

func field(body []byte, name string) string {
	re := regexp.MustCompile(`['"]` + regexp.QuoteMeta(name) + `['"]\s*:\s*['"]([^'"]+)['"]`)
	m := re.FindSubmatch(body)
	if len(m) == 2 {
		return string(m[1])
	}
	return ""
}

func intField(body []byte, name, fallback string) string {
	re := regexp.MustCompile(`['"]` + regexp.QuoteMeta(name) + `['"]\s*:\s*([0-9]+)`)
	m := re.FindSubmatch(body)
	if len(m) == 2 {
		if _, err := strconv.Atoi(string(m[1])); err == nil {
			return string(m[1])
		}
	}
	return fallback
}

func defaultPort(port string) string {
	if port == "" {
		return "443"
	}
	return port
}
