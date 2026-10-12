package service

import (
	"fmt"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
)

func (s *JMService) CreateShareRoom(data model.SharingSessionRequest) (res model.SharingSession, err error) {
	_, err = s.authClient.Post(ShareCreateURL, data, &res)
	return
}

func (s *JMService) GetShareUserInfo(query string) (res []*model.MiniUser, err error) {
	params := make(map[string]string)
	params["action"] = "suggestion"
	params["search"] = query
	params["limit"] = "100"
	var paginationRes PaginationResult[*model.MiniUser]
	_, err = s.authClient.Get(UserListURL, &paginationRes, params)
	if err != nil {
		return
	}
	res = make([]*model.MiniUser, 0, 50)
	res = append(res, paginationRes.Results...)
	for paginationRes.NextURL != "" {
		paginationRes, err = s.GetNextPaginationUserInfo(paginationRes.NextURL)
		if err != nil {
			return
		}
		res = append(res, paginationRes.Results...)
	}
	return
}

func (s *JMService) GetSuggestionUsers(query string) (res []*model.MiniUser, err error) {
	params := make(map[string]string)
	params["search"] = query
	params["limit"] = "10"
	var paginationRes PaginationResult[*model.MiniUser]
	_, err = s.authClient.Get(UserSuggestionsURL, &paginationRes, params)
	if err != nil {
		return
	}
	res = make([]*model.MiniUser, 0, 50)
	res = append(res, paginationRes.Results...)
	for paginationRes.NextURL != "" {
		paginationRes, err = s.GetNextPaginationUserInfo(paginationRes.NextURL)
		if err != nil {
			return
		}
		res = append(res, paginationRes.Results...)
	}
	return
}

func (s *JMService) GetNextPaginationUserInfo(reqUrl string) (res PaginationResult[*model.MiniUser], err error) {
	result := TrimHost(reqUrl)
	_, err = s.authClient.Get(result, &res)
	return
}

func (s *JMService) JoinShareRoom(data model.SharePostData) (res model.ShareRecord, err error) {
	_, err = s.authClient.Post(ShareSessionJoinURL, data, &res)
	return
}

func (s *JMService) FinishShareRoom(recordId string) (err error) {
	reqUrl := fmt.Sprintf(ShareSessionFinishURL, recordId)
	_, err = s.authClient.Patch(reqUrl, nil, nil)
	return
}

func (s *JMService) SyncUserKokoPreference(cookies map[string]string, data model.UserKokoPreference) (err error) {
	/*
		csrfToken 存储在 cookies 中
		其 使用的名称 name 为 `{SESSION_COOKIE_NAME_PREFIX}csrftoken` 动态组成
	*/
	var (
		csrfToken  string
		namePrefix string
	)
	checkNamePrefixValid := func(name string) bool {
		invalidStrings := []string{`""`, `''`}
		for _, invalidString := range invalidStrings {
			if strings.Contains(name, invalidString) {
				return false
			}
		}
		return true
	}
	namePrefix = cookies["SESSION_COOKIE_NAME_PREFIX"]
	csrfCookieName := "csrftoken"
	if namePrefix != "" && checkNamePrefixValid(namePrefix) {
		csrfCookieName = namePrefix + csrfCookieName
	}
	csrfToken = cookies[csrfCookieName]
	client := s.authClient.Clone()
	client.SetHeader("X-CSRFToken", csrfToken)
	for k, v := range cookies {
		client.SetCookie(k, v)
	}
	_, err = client.Patch(UserKoKoPreferenceURL, data, nil)
	return
}

type PaginationResult[T any] struct {
	Count    int    `json:"count"`
	Results  []T    `json:"results"`
	Previous string `json:"previous"`
	NextURL  string `json:"next"`
}
