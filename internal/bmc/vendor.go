package bmc

import (
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
	return planJNLP(c, ProfileHP, []string{
		"/html/java_irc.html",
		"/html/intgapp4_231.jar",
		"/html/remcons.html",
		"/dvc_java.jnlp",
		"/irc.jnlp",
		"/console.jnlp",
		"/remote_console.jnlp",
	}, isKVMJNLP)
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
