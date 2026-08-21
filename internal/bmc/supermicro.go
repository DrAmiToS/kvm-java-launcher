package bmc

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func PlanSupermicro(c *Client) (*LaunchPlan, error) {
	paths := append([]string{
		"/cgi/url_redirect.cgi?url_name=ikvm&url_type=jwsk",
		"/cgi/url_redirect.cgi?url_name=ikvm&url_type=jnlp",
		"/Java/jviewer.jnlp",
		"/Java/ikvm.jnlp",
	}, genericJNLPPaths()...)
	if err := LoginSupermicro(c); err == nil {
		fmt.Println("Supermicro web login: SID cookie received")
	} else {
		fmt.Println("Supermicro web login: not confirmed:", err)
	}
	return planJNLP(c, ProfileSupermicro, paths, isKVMJNLP)
}

func PlanSupermicroSOL(c *Client) (*LaunchPlan, error) {
	paths := []string{
		"/cgi/url_redirect.cgi?url_name=sol&url_type=jwss",
		"/cgi/url_redirect.cgi?url_name=sol&url_type=jnlp",
	}
	if err := LoginSupermicro(c); err == nil {
		fmt.Println("Supermicro web login: SID cookie received")
	} else {
		fmt.Println("Supermicro web login: not confirmed:", err)
	}
	return planJNLP(c, ProfileSupermicroSOL, paths, isSOLJNLP)
}

func PlanGenericJNLP(c *Client, profile Profile) (*LaunchPlan, error) {
	return planJNLP(c, profile, genericJNLPPaths(), nil)
}

func planJNLP(c *Client, profile Profile, paths []string, accept func([]byte) bool) (*LaunchPlan, error) {
	plan, errs := tryJNLPPaths(c, profile, paths, accept)
	if plan != nil {
		return plan, nil
	}
	if !c.SchemeExplicit && c.BaseURL.Scheme == "https" {
		httpClient := c.WithScheme("http")
		httpPlan, httpErrs := tryJNLPPaths(httpClient, profile, paths, accept)
		if httpPlan != nil {
			return httpPlan, nil
		}
		errs = append(errs, httpErrs...)
	}
	if len(errs) == 0 {
		return nil, errors.New("JNLP endpoint was not found")
	}
	return nil, fmt.Errorf("JNLP endpoint was not found (%s)", strings.Join(errs, "; "))
}

func tryJNLPPaths(c *Client, profile Profile, paths []string, accept func([]byte) bool) (*LaunchPlan, []string) {
	var errs []string
	for _, path := range paths {
		headers := addHostHeader(addSIDHeader(supermicroLaunchHeaders(path), c.SID), c.HostHeader)
		body, err := c.GetWithHeaders(path, headers)
		if err == nil && looksLikeJNLP(body) && (accept == nil || accept(body)) {
			return &LaunchPlan{Profile: profile, JNLPURL: c.URL(path), JNLPBody: body, Headers: headers}, nil
		}
		if err != nil {
			errs = append(errs, c.URL(path)+": "+err.Error())
			continue
		}
		if looksLikeJNLP(body) {
			errs = append(errs, c.URL(path)+": JNLP was rejected by profile filter: "+responseSnippet(body))
		} else {
			errs = append(errs, c.URL(path)+": response was not JNLP: "+responseSnippet(body))
		}
	}
	return nil, errs
}

func supermicroLaunchHeaders(path string) http.Header {
	headers := http.Header{}
	if strings.Contains(path, "/cgi/") {
		headers.Set("Referer", "http://localhost")
		headers.Set("User-Agent", "Mozilla/5.0")
	}
	return headers
}

func addSIDHeader(headers http.Header, sid string) http.Header {
	if sid != "" {
		headers.Set("Cookie", "SID="+sid)
	}
	return headers
}

func addHostHeader(headers http.Header, host string) http.Header {
	if host != "" {
		headers.Set("Host", host)
	}
	return headers
}

func LoginSupermicro(c *Client) error {
	attempts := []url.Values{
		{"name": {c.Username}, "pwd": {c.Password}},
		{"USERNAME": {c.Username}, "PASSWORD": {c.Password}},
		{"user": {c.Username}, "password": {c.Password}},
		{"WEBVAR_USERNAME": {c.Username}, "WEBVAR_PASSWORD": {c.Password}},
	}
	var lastErr error
	for _, form := range attempts {
		body, _, cookies, err := c.PostFormResponse("/cgi/login.cgi", form, http.Header{
			"Referer":    {"http://localhost"},
			"User-Agent": {"Mozilla/5.0"},
		})
		if err != nil {
			lastErr = err
			continue
		}
		text := strings.ToLower(string(body))
		if strings.Contains(text, "login failed") || strings.Contains(text, "invalid password") {
			lastErr = errors.New("Supermicro login failed")
			continue
		}
		if sid := sidCookie(cookies); sid != "" {
			c.SID = sid
			c.SetCookie("SID", sid)
			return nil
		}
		lastErr = fmt.Errorf("Supermicro login did not return SID cookie: %s", responseSnippet(body))
	}
	return lastErr
}

func sidCookie(cookies []*http.Cookie) string {
	for _, cookie := range cookies {
		if strings.EqualFold(cookie.Name, "SID") && cookie.Value != "" {
			return cookie.Value
		}
	}
	return ""
}

func genericJNLPPaths() []string {
	return []string{
		"/viewer.jnlp",
		"/launch.jnlp",
		"/console.jnlp",
		"/kvm.jnlp",
		"/ikvm.jnlp",
		"/Java/jviewer.jnlp",
		"/Java/ikvm.jnlp",
		"/Java/launch.jnlp",
		"/Java/console.jnlp",
		"/cgi/url_redirect.cgi?url_name=ikvm&url_type=jwsk",
		"/cgi/url_redirect.cgi?url_name=ikvm&url_type=jnlp",
		"/cgi/url_redirect.cgi?url_name=sol&url_type=jwss",
		"/cgi/url_redirect.cgi?url_name=sol&url_type=jnlp",
		"/cgi/url_redirect.cgi?url_name=console&url_type=jnlp",
		"/cgi/url_redirect.cgi?url_name=console&url_type=jwss",
		"/cgi/login.cgi?url_name=ikvm&url_type=jnlp",
		"/cgi/login.cgi?url_name=sol&url_type=jwss",
		"/cgi-bin/launchjnlp.cgi",
		"/cgi-bin/kvm.jnlp",
		"/cgi-bin/ikvm.jnlp",
	}
}

func looksLikeJNLP(body []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if start, ok := token.(xml.StartElement); ok {
			return strings.EqualFold(start.Name.Local, "jnlp")
		}
	}
}

func isKVMJNLP(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "ikvm") ||
		strings.Contains(text, "jviewer") ||
		strings.Contains(text, "kvm") ||
		strings.Contains(text, "viewer")
}

func isSOLJNLP(body []byte) bool {
	return strings.Contains(strings.ToLower(string(body)), "sol")
}

func responseSnippet(body []byte) string {
	text := strings.TrimSpace(string(body))
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	if len(text) > 180 {
		text = text[:180] + "..."
	}
	if text == "" {
		return "empty response"
	}
	return fmt.Sprintf("%q", text)
}
