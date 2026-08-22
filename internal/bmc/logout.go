package bmc

import (
	"fmt"
	"net/http"
)

func Logout(c *Client, profile Profile, postExitPath string) error {
	switch profile {
	case ProfileDell:
		path := postExitPath
		if path == "" {
			path = "/data/logout"
		}
		_, _, _, err := c.PostRawResponse(path, "", nil)
		return err
	case ProfileSupermicro, ProfileSupermicroSOL:
		return logoutFirstOK(c, []logoutAttempt{
			{method: http.MethodGet, path: "/cgi/logout.cgi"},
			{method: http.MethodPost, path: "/cgi/logout.cgi"},
			{method: http.MethodGet, path: "/cgi/logout.cgi?SID=" + c.SID},
		})
	case ProfileHP:
		return logoutFirstOK(c, []logoutAttempt{
			{method: http.MethodDelete, path: "/json/login_session"},
			{method: http.MethodPost, path: "/json/logout_session", body: `{"method":"logout"}`},
			{method: http.MethodPost, path: "/json/login_session", body: `{"method":"logout"}`},
		})
	case ProfileAMIMegaRAC:
		return logoutFirstOK(c, []logoutAttempt{
			{method: http.MethodPost, path: "/rpc/WEBSES/logout.asp"},
			{method: http.MethodGet, path: "/rpc/WEBSES/logout.asp"},
			{method: http.MethodPost, path: "/rpc/WEBSES/logout"},
		})
	default:
		return nil
	}
}

type logoutAttempt struct {
	method string
	path   string
	body   string
}

func logoutFirstOK(c *Client, attempts []logoutAttempt) error {
	var lastErr error
	for _, attempt := range attempts {
		var err error
		switch attempt.method {
		case http.MethodGet:
			_, err = c.Get(attempt.path)
		case http.MethodDelete:
			_, _, _, err = c.DeleteRawResponse(attempt.path, attempt.body, nil)
		default:
			_, _, _, err = c.PostRawResponse(attempt.path, attempt.body, nil)
		}
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("%s %s: %w", attempt.method, attempt.path, err)
	}
	return lastErr
}
