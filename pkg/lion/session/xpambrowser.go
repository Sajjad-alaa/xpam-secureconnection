package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/config"
)

// XPAM: web assets shown through Lion. The browser service runs one isolated Chromium per session
// in kiosk mode (no address bar) and returns a VNC endpoint, like a virtual app.
const connectWebBrowser = "web_browser"

type virtualAppReleaser interface {
	ReleaseContainer(id string) error
}

type browserClient struct {
	base string
	key  string
	http *http.Client
}

func newBrowserClient(cfg config.Config) (*browserClient, error) {
	if strings.TrimSpace(cfg.BrowserServiceKey) == "" {
		return nil, errors.New("the browser service is not set up (BROWSER_KEY)")
	}
	base := strings.TrimRight(cfg.BrowserServiceURL, "/")
	if base == "" {
		base = "http://browser:9001"
	}
	return &browserClient{base: base, key: cfg.BrowserServiceKey, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

func (c *browserClient) post(path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("browser service: %s", e.Error)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *browserClient) ReleaseContainer(id string) error {
	return c.post("/sessions/"+url.PathEscape(id)+"/release", map[string]string{}, nil)
}

// browserTargetURL builds the site address from the asset address and its http/https protocol port.
func browserTargetURL(token *model.ConnectToken) (string, error) {
	raw := strings.TrimSpace(token.Asset.Address)
	protocol := strings.ToLower(token.Protocol)
	if protocol != "https" {
		protocol = "http"
	}
	if !strings.Contains(raw, "://") {
		raw = protocol + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", errors.New("the web asset address is not a valid HTTP/HTTPS address")
	}
	if u.Port() == "" {
		for _, p := range token.Asset.Protocols {
			name := strings.ToLower(p.Name)
			if (name == "http" || name == "https") && p.Port > 0 &&
				!((u.Scheme == "http" && p.Port == 80) || (u.Scheme == "https" && p.Port == 443)) {
				u.Host = u.Hostname() + ":" + strconv.Itoa(p.Port)
				break
			}
		}
	}
	u.Fragment = ""
	return u.String(), nil
}

func (s *Server) startXPAMBrowser(token *model.ConnectToken, width, height int) (*model.VirtualAppContainer, *browserClient, error) {
	client, err := newBrowserClient(config.GetConf())
	if err != nil {
		return nil, nil, err
	}
	target, err := browserTargetURL(token)
	if err != nil {
		return nil, nil, err
	}
	spec := token.Asset.SpecInfo
	secret := ""
	if token.Account.SecretType.Value == "password" || token.Account.SecretType.Value == "" {
		secret = token.Account.Secret
	}
	body := map[string]any{
		"url":                  target,
		"width":                width,
		"height":               height,
		"username":             token.Account.Username,
		"password":             secret,
		"autofill":             spec.Autofill,
		"username_selector":    spec.UsernameSelector,
		"password_selector":    spec.PasswordSelector,
		"submit_selector":      spec.SubmitSelector,
		"success_selector":     spec.SuccessSelector,
		"interactive_selector": spec.InteractiveSelector,
		"script":               spec.Script,
		"allowed_urls":         spec.AllowedURLs,
		"ignore_cert":          !spec.VerifyCert, // XPAM: per asset (verify the site's certificate)
	}
	if strings.HasPrefix(token.Account.Username, "@") { // no real account (e.g. manual input not supported here)
		body["username"], body["password"] = "", ""
	}
	var container model.VirtualAppContainer
	if err = client.post("/sessions", body, &container); err != nil {
		return nil, nil, err
	}
	return &container, client, nil
}
