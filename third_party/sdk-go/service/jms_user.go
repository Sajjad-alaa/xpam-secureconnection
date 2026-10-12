package service

import (
	"fmt"
	"github.com/jumpserver-dev/sdk-go/model"
	"net/http"
)

func (s *JMService) CheckUserCookie(cookies map[string]string) (user *model.User, err error) {
	client := s.authClient.Clone()
	for k, v := range cookies {
		client.SetCookie(k, v)
	}
	_, err = client.Get(UserProfileURL, &user)
	return
}

func (s *JMService) GetUserByUsername(username string) (user *model.User, err error) {
	reqURL := fmt.Sprintf(UserDetailURL, username)
	_, err = s.authClient.Get(reqURL, &user)
	return
}

// video worker
func (s *JMService) CheckUserHeaders(header map[string]string) (user *model.User, err error) {
	client := s.authClient.Clone()
	for k, v := range header {
		client.SetHeader(k, v)
	}
	_, err = client.Get(UserProfileURL, &user)
	return
}

func (s *JMService) CheckComponentProfile(req *http.Request) (user *model.User, err error) {
	header := req.Header
	client := s.authClient.Clone()
	allHeaders := []string{AuthorizationHeader, DateHeader, orgHeaderKey}
	for i := range allHeaders {
		name := allHeaders[i]
		client.SetHeader(name, header.Get(name))
	}
	_, err = client.Get(UserProfileURL, &user)
	return
}

const (
	AuthorizationHeader = "Authorization"
	DateHeader          = "Date"
)
