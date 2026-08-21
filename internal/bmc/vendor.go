package bmc

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func PlanDell(c *Client) (*LaunchPlan, error) {
	if st1, err := LoginDell(c); err == nil {
		fmt.Println("Dell iDRAC web login: launch token received")
		host := c.BaseURL.Hostname()
		paths := append(dellViewerPaths(host, st1), dellJNLPPaths()...)
		plan, err := planJNLP(c, ProfileDell, paths, isKVMJNLP)
		if err != nil {
			return nil, err
		}
		plan.PostExitPath = "/data/logout"
		return plan, nil
	} else {
		var authErr *DellAuthError
		if errors.As(err, &authErr) {
			return nil, err
		}
		fmt.Println("Dell iDRAC web login: not confirmed:", err)
	}
	return planJNLP(c, ProfileDell, dellJNLPPaths(), isKVMJNLP)
}

func LoginDell(c *Client) (string, error) {
	bodyText := "user=" + url.QueryEscape(c.Username) + "&password=" + url.QueryEscape(c.Password)
	endpoints := []string{
		"/data/login",
		"/data/login?console",
	}
	var lastErr error
	for _, endpoint := range endpoints {
		consoleLogin := strings.Contains(endpoint, "?console")
		_, _, _, _ = c.PostRawResponse("/data/logout", "", dellLoginHeaders(c, consoleLogin))
		body, _, cookies, err := c.PostRawResponse(endpoint, bodyText, dellLoginHeaders(c, consoleLogin))
		if err != nil {
			lastErr = err
			continue
		}
		if st1 := cookieValue(cookies, "ST1"); st1 != "" {
			c.SetCookie("ST1", st1)
			c.SetCookie("tokenvalue", st1)
			return st1, nil
		}
		if st1 := dellST1FromBody(body); st1 != "" {
			c.SetCookie("ST1", st1)
			c.SetCookie("tokenvalue", st1)
			return st1, nil
		}
		if authResult := dellAuthResultFromBody(body); authResult != "" {
			lastErr = &DellAuthError{
				Code:    authResult,
				Message: dellAuthResultMessage(authResult),
				Body:    responseSnippet(body),
			}
			if authResult == "5" {
				return "", lastErr
			}
			continue
		}
		lastErr = fmt.Errorf("iDRAC login did not return ST1 token: %s", responseSnippet(body))
	}
	return "", lastErr
}

type DellAuthError struct {
	Code    string
	Message string
	Body    string
}

func (e *DellAuthError) Error() string {
	return fmt.Sprintf("iDRAC login authResult=%s (%s): %s", e.Code, e.Message, e.Body)
}

func dellLoginHeaders(c *Client, consoleLogin bool) http.Header {
	origin := dellLoginOrigin(c)
	referer := origin + "/login.html"
	if consoleLogin {
		referer += "?console"
	}
	return http.Header{
		"Accept":     {"*/*"},
		"Origin":     {origin},
		"Referer":    {referer},
		"User-Agent": {"Mozilla/5.0"},
		"Cookie":     {"-http-session-=; sysidledicon=ledIcon%20grayLed"},
	}
}

func dellLoginOrigin(c *Client) string {
	host := c.BaseURL.Host
	if c.HostHeader != "" {
		host = c.HostHeader
	}
	return c.BaseURL.Scheme + "://" + host
}

func dellST1FromBody(body []byte) string {
	text := string(body)
	patterns := []string{
		`ST1=([^,"'<>&\s]+)`,
		`(?i)<ST1>([^<]+)</ST1>`,
		`(?i)<sid>([^<]+)</sid>`,
		`(?i)"ST1"\s*:\s*"([^"]+)"`,
	}
	for _, pattern := range patterns {
		m := regexp.MustCompile(pattern).FindStringSubmatch(text)
		if len(m) == 2 {
			value, err := url.QueryUnescape(strings.TrimSpace(m[1]))
			if err == nil {
				return value
			}
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func dellAuthResultFromBody(body []byte) string {
	m := regexp.MustCompile(`(?i)<authResult>\s*([^<\s]+)\s*</authResult>`).FindSubmatch(body)
	if len(m) != 2 {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func dellAuthResultMessage(code string) string {
	switch code {
	case "0":
		return "success"
	case "1":
		return "login blocked temporarily after failed attempts"
	case "2":
		return "missing username"
	case "3":
		return "missing password"
	case "4":
		return "insufficient privilege"
	case "5":
		return "iDRAC web session count exceeded"
	case "99":
		return "login failed; iDRAC did not accept the submitted credentials/session"
	default:
		return "unknown iDRAC login result"
	}
}

func dellViewerPaths(host, st1 string) []string {
	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	return []string{
		"/viewer.jnlp(" + host + "@0@" + host + "@@" + now + "@ST1=" + url.QueryEscape(st1) + ")",
		"/viewer.jnlp(" + host + "@0@" + host + "@PowerEdge@" + now + "@ST1=" + url.QueryEscape(st1) + ")",
	}
}

func dellJNLPPaths() []string {
	return []string{
		"/viewer.jnlp",
		"/console.jnlp",
		"/start.jnlp",
		"/cgi-bin/webcgi/launchKVM",
		"/data?get=console",
		"/sysmgmt/2015/server/console",
	}
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.Value != "" {
			return cookie.Value
		}
	}
	return ""
}

func PlanHP(c *Client) (*LaunchPlan, error) {
	sessionKey, err := LoginHP(c)
	if err != nil {
		fmt.Println("HPE iLO web login: not confirmed:", err)
		return planJNLP(c, ProfileHP, hpJNLPPaths(), isKVMJNLP)
	}
	fmt.Println("HPE iLO web login: session key received")
	logHPLicense(c)
	body := hpAppletJNLP(c, sessionKey, "/html/intgapp4_231.jar", "en")
	return &LaunchPlan{
		Profile:  ProfileHP,
		JNLPURL:  c.URL("/html/java_irc.html"),
		JNLPBody: []byte(body),
		Headers:  addHostHeader(http.Header{}, c.HostHeader),
	}, nil
}

func LoginHP(c *Client) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"method":     "login",
		"user_login": c.Username,
		"password":   c.Password,
	})
	if err != nil {
		return "", err
	}
	body, _, _, err := c.PostRawResponse("/json/login_session", string(payload), http.Header{
		"Accept":       {"application/json, text/javascript, */*; q=0.01"},
		"Content-Type": {"application/json"},
		"User-Agent":   {"Mozilla/5.0"},
	})
	if err != nil {
		return "", err
	}
	var parsed struct {
		SessionKey string `json:"session_key"`
		Error      string `json:"error"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.SessionKey != "" {
			c.SetCookie("session_key", parsed.SessionKey)
			return parsed.SessionKey, nil
		}
		if parsed.Error != "" || parsed.Message != "" {
			return "", fmt.Errorf("iLO login failed: %s %s", parsed.Error, parsed.Message)
		}
	}
	if key := hpSessionKeyFromBody(body); key != "" {
		c.SetCookie("session_key", key)
		return key, nil
	}
	return "", fmt.Errorf("iLO login did not return session_key: %s", responseSnippet(body))
}

func logHPLicense(c *Client) {
	license, err := HPLicense(c)
	if err != nil {
		fmt.Println("HPE iLO license: unknown (" + err.Error() + ")")
		return
	}
	if license == "" {
		fmt.Println("HPE iLO license: unknown")
		return
	}
	fmt.Println("HPE iLO license:", license)
	if !strings.Contains(strings.ToLower(license), "advanced") {
		fmt.Println("HPE iLO warning: Integrated Remote Console usually requires iLO Advanced")
	}
}

func HPLicense(c *Client) (string, error) {
	body, err := c.GetWithHeaders("/json/overview", http.Header{
		"Accept":     {"application/json, text/javascript, */*; q=0.01"},
		"User-Agent": {"Mozilla/5.0"},
	})
	if err != nil {
		return "", err
	}
	return hpLicenseFromBody(body)
}

func hpLicenseFromBody(body []byte) (string, error) {
	var parsed struct {
		License string `json:"license"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	return strings.TrimSpace(parsed.License), nil
}

func hpSessionKeyFromBody(body []byte) string {
	patterns := []string{
		`"session_key"\s*:\s*"([^"]+)"`,
		`(?i)<session_key>([^<]+)</session_key>`,
	}
	text := string(body)
	for _, pattern := range patterns {
		m := regexp.MustCompile(pattern).FindStringSubmatch(text)
		if len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func hpAppletJNLP(c *Client, sessionKey, jarPath, lang string) string {
	if lang == "" {
		lang = "en"
	}
	codebase := c.BaseURL.ResolveReference(&url.URL{Path: "/"}).String()
	documentBase := c.URL("/html/java_irc.html?lang=" + url.QueryEscape(lang))
	return `<?xml version="1.0" encoding="UTF-8"?>
<jnlp spec="1.0+" codebase="` + xmlEscape(codebase) + `" href="">
  <information>
    <title>Integrated Remote Console</title>
    <vendor>HPE</vendor>
  </information>
  <security>
    <all-permissions/>
  </security>
  <resources>
    <j2se version="1.5+"/>
    <jar href="` + xmlEscape(strings.TrimPrefix(jarPath, "/")) + `" main="false"/>
  </resources>
  <applet-desc main-class="com.hp.ilo2.intgapp.intgapp" name="iLOJIRC" documentbase="` + xmlEscape(documentBase) + `" width="1" height="1">
    <param name="RCINFO1" value="` + xmlEscape(sessionKey) + `"/>
    <param name="RCINFOLANG" value="` + xmlEscape(lang) + `"/>
    <param name="INFO0" value="7AC3BDEBC9AC64E85734454B53BB73CE"/>
    <param name="INFO1" value="17988"/>
    <param name="INFO2" value="composite"/>
  </applet-desc>
  <update check="background"/>
</jnlp>
`
}

func xmlEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func hpJNLPPaths() []string {
	return []string{
		"/dvc_java.jnlp",
		"/irc.jnlp",
		"/console.jnlp",
		"/remote_console.jnlp",
	}
}

func PlanLenovo(c *Client) (*LaunchPlan, error) {
	return planJNLP(c, ProfileLenovo, []string{
		"/viewer.jnlp",
		"/console.jnlp",
		"/remoteconsole.jnlp",
		"/designs/imm/viewer.jnlp",
		"/designs/imm2/viewer.jnlp",
		"/designs/xcc/viewer.jnlp",
		"/data?get=console",
	}, isKVMJNLP)
}
