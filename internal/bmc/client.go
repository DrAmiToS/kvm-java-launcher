package bmc

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	BaseURL        *url.URL
	HTTP           *http.Client
	Username       string
	Password       string
	SchemeExplicit bool
	SID            string
	HostHeader     string
}

func New(rawURL, username, password string) (*Client, error) {
	enableLegacyTLS()
	schemeExplicit := strings.Contains(rawURL, "://")
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, errors.New("empty BMC host")
	}
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS10,
			MaxVersion:         tls.VersionTLS12,
			Renegotiation:      tls.RenegotiateOnceAsClient,
			CipherSuites: []uint16{
				tls.TLS_RSA_WITH_RC4_128_SHA,
				tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
				tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_128_CBC_SHA,
				tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
				tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
				tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			},
			CurvePreferences: []tls.CurveID{tls.CurveP256, tls.CurveP384, tls.CurveP521},
		},
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &Client{
		BaseURL:        u,
		HTTP:           &http.Client{Jar: jar, Timeout: 25 * time.Second, Transport: tr},
		Username:       username,
		Password:       password,
		SchemeExplicit: schemeExplicit,
	}, nil
}

func enableLegacyTLS() {
	current := os.Getenv("GODEBUG")
	for _, flag := range []string{"tlsrsakex=1", "tls3des=1"} {
		if strings.Contains(current, flag) {
			continue
		}
		if current != "" {
			current += ","
		}
		current += flag
	}
	_ = os.Setenv("GODEBUG", current)
}

func (c *Client) WithScheme(scheme string) *Client {
	clone := *c
	u := *c.BaseURL
	u.Scheme = scheme
	clone.BaseURL = &u
	return &clone
}

func (c *Client) SetHostHeader(host string) {
	host = strings.TrimSpace(host)
	c.HostHeader = host
	if host == "" {
		return
	}
	c.HTTP.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		req.Host = host
		return http.ErrUseLastResponse
	}
	if tr, ok := c.HTTP.Transport.(*http.Transport); ok && tr.TLSClientConfig != nil {
		tlsConfig := tr.TLSClientConfig.Clone()
		tlsConfig.ServerName = stripPort(host)
		tr.TLSClientConfig = tlsConfig
	}
}

func (c *Client) SetCookie(name, value string) {
	if c.HTTP.Jar == nil || value == "" {
		return
	}
	c.HTTP.Jar.SetCookies(c.BaseURL, []*http.Cookie{
		{Name: name, Value: value, Path: "/"},
	})
}

func (c *Client) URL(path string) string {
	ref, err := url.Parse(path)
	if err != nil {
		ref = &url.URL{Path: path}
	}
	return c.BaseURL.ResolveReference(ref).String()
}

func (c *Client) Get(path string) ([]byte, error) {
	return c.GetWithHeaders(path, nil)
}

func (c *Client) GetWithHeaders(path string, headers http.Header) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.URL(path), nil)
	if err != nil {
		return nil, err
	}
	c.applyRequestHost(req)
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) PostFormResponse(path string, values url.Values, headers http.Header) ([]byte, http.Header, []*http.Cookie, error) {
	return c.PostRawResponse(path, values.Encode(), headers)
}

func (c *Client) PostRawResponse(path, bodyText string, headers http.Header) ([]byte, http.Header, []*http.Cookie, error) {
	req, err := http.NewRequest(http.MethodPost, c.URL(path), bytes.NewBufferString(bodyText))
	if err != nil {
		return nil, nil, nil, err
	}
	c.applyRequestHost(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, resp.Header, resp.Cookies(), errors.New(resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	return body, resp.Header, resp.Cookies(), err
}

func (c *Client) applyRequestHost(req *http.Request) {
	if c.HostHeader != "" {
		req.Host = c.HostHeader
	}
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func (c *Client) PostForm(path string, values url.Values) ([]byte, error) {
	return c.PostFormWithHeaders(path, values, nil)
}

func (c *Client) PostFormWithHeaders(path string, values url.Values, headers http.Header) ([]byte, error) {
	body, _, _, err := c.PostFormResponse(path, values, headers)
	return body, err
}
