package service

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jumpserver-dev/sdk-go/httplib"
	"github.com/jumpserver-dev/sdk-go/model"
)

var AccessKeyUnauthorized = errors.New("access key unauthorized")

var ConnectErr = errors.New("api connect err")

const (
	minTimeOut = time.Second * 30

	orgHeaderKey   = "X-JMS-ORG"
	orgHeaderValue = "ROOT"
)

func NewAuthJMService(opts ...Option) (*JMService, error) {
	opt := option{
		CoreHost: "http://127.0.0.1:8080",
		TimeOut:  time.Minute,
	}
	for _, setter := range opts {
		setter(&opt)
	}
	if opt.TimeOut < minTimeOut {
		opt.TimeOut = minTimeOut
	}
	httpOpts := make([]httplib.Opt, 0)
	if opt.Insecure {
		httpOpts = append(httpOpts, httplib.WithInsecure())
	}
	httpClient, err := httplib.NewClient(opt.CoreHost, opt.TimeOut, httpOpts...)
	if err != nil {
		return nil, err
	}
	if opt.sign != nil {
		httpClient.SetAuthSign(opt.sign)
	}
	httpClient.SetHeader(orgHeaderKey, orgHeaderValue)
	return &JMService{authClient: httpClient, opt: &opt}, nil
}

type JMService struct {
	authClient *httplib.Client
	opt        *option

	sync.Mutex
}

func (s *JMService) RegisterTerminal(name, token, componentName string) (res model.Terminal, err error) {
	data := map[string]string{"name": name, "comment": componentName, "type": componentName}
	s.authClient.SetAuthSign(&httplib.CustomAuth{
		AuthScheme: "BootstrapToken",
		Token:      token,
	})
	_, err = s.authClient.Post(TerminalRegisterURL, data, &res)
	return
}

func (s *JMService) GetUserById(userID string) (user *model.User, err error) {
	reqURL := fmt.Sprintf(UserDetailURL, userID)
	_, err = s.authClient.Get(reqURL, &user)
	return
}

func (s *JMService) GetProfile() (user *model.User, err error) {
	var res *http.Response
	res, err = s.authClient.Get(UserProfileURL, &user)
	if res == nil && err != nil {
		return nil, fmt.Errorf("%w:%s", ConnectErr, err.Error())
	}
	if res != nil && res.StatusCode == http.StatusUnauthorized {
		return user, AccessKeyUnauthorized
	}
	return user, err
}

func (s *JMService) GetTerminalConfig() (conf model.TerminalConfig, err error) {
	_, err = s.authClient.Get(TerminalConfigURL, &conf)
	return
}

func (s *JMService) CloneClient() httplib.Client {
	return s.authClient.Clone()
}

func (s *JMService) Copy() *JMService {
	client := s.authClient.Clone()
	if s.opt.sign != nil {
		client.SetAuthSign(s.opt.sign)
	}
	client.SetHeader(orgHeaderKey, orgHeaderValue)
	return &JMService{
		authClient: &client,
		opt:        s.opt,
	}
}

func (s *JMService) SetCookie(name, value string) {
	s.authClient.SetCookie(name, value)
}

func (s *JMService) SetHeader(key, value string) {
	s.authClient.SetHeader(key, value)
}

func (s *JMService) GetWsClient() (*websocket.Conn, error) {
	u, err := url.Parse(s.opt.CoreHost)
	if err != nil {
		return nil, err
	}
	scheme := "ws"
	switch u.Scheme {
	case "http":
		scheme = "ws"
	case "https":
		scheme = "wss"
	}
	wsReqURL := url.URL{Scheme: scheme, Host: u.Host, Path: wsURL}
	req, err := http.NewRequest(http.MethodGet, wsReqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	if s.opt.sign != nil {
		if err = s.opt.sign.Sign(req); err != nil {
			return nil, err
		}
	}
	header := req.Header
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}
	if s.opt.Insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	c, _, err := dialer.Dial(wsReqURL.String(), header)
	if err != nil {
		return nil, err
	}
	return c, nil
}

const (
	wsURL = "ws/terminal-task/"
)

func (s *JMService) Call(method, reqUrl string, data, res interface{},
	params ...map[string]string) (resp *http.Response, err error) {
	method = strings.ToUpper(method)
	return s.authClient.Do(method, reqUrl, data, res, params...)
}
