package service

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
)

func (s *JMService) SearchPermAsset(userId, key string) (res model.PermAssetList, err error) {
	reqUrl := fmt.Sprintf(UserPermsAssetsURL, userId)
	payload := map[string]string{"search": key, "limit": "100"}
	var ret model.PaginationResponse
	assets := make([]model.PermAsset, 0, 100)
	_, err = s.authClient.Get(reqUrl, &ret, payload)
	if err != nil {
		return
	}
	assets = append(assets, ret.Data...)
	for ret.NextURL != "" {
		ret, err = s.GetNextURLPermAssets(ret.NextURL)
		if err != nil {
			return
		}
		assets = append(assets, ret.Data...)
	}
	res = model.PermAssetList(assets)
	return
}

func (s *JMService) GetUserPermAssetDetailById(userId, assetId string) (resp model.PermAssetDetail, err error) {
	reqUrl := fmt.Sprintf(UserPermsAssetAccountsURL, userId, assetId)
	_, err = s.authClient.Get(reqUrl, &resp)
	return
}

func (s *JMService) GetAllUserPermsAssets(userId string) ([]model.PermAsset, error) {
	var params = model.PaginationParam{
		PageSize: 100,
		Offset:   0,
	}
	assets := make([]model.PermAsset, 0, 100)
	res, err := s.GetUserPermsAssets(userId, params)
	if err != nil {
		return nil, err
	}
	assets = append(assets, res.Data...)
	for res.NextURL != "" {
		res, err = s.GetNextURLPermAssets(res.NextURL)
		if err != nil {
			return nil, err
		}
		assets = append(assets, res.Data...)
	}
	return assets, nil
}

func (s *JMService) GetUserPermsAssets(userId string, params model.PaginationParam) (resp model.PaginationResponse, err error) {
	reqUrl := fmt.Sprintf(UserPermsAssetsURL, userId)
	return s.getPaginationAssets(reqUrl, params)
}

func (s *JMService) RefreshUserAllPermsAssets(userId string) ([]model.PermAsset, error) {
	var params = model.PaginationParam{
		PageSize: 100,
		Offset:   0,
		Refresh:  true,
	}
	assets := make([]model.PermAsset, 0, 100)
	res, err := s.GetUserPermsAssets(userId, params)
	if err != nil {
		return nil, err
	}
	assets = append(assets, res.Data...)
	for res.NextURL != "" {
		res, err = s.GetNextURLPermAssets(res.NextURL)
		if err != nil {
			return nil, err
		}
		assets = append(assets, res.Data...)
	}
	return assets, nil
}

func (s *JMService) GetUserPermAssetsByIP(userId, assetIP string) (assets []model.PermAsset, err error) {
	params := map[string]string{
		"address": assetIP,
	}
	return s.SearchUserPermAssets(userId, params)
}

func (s *JMService) GetUserPermAssetsById(userId, assetId string) (assets []model.PermAsset, err error) {
	params := map[string]string{
		"id": assetId,
	}
	return s.SearchUserPermAssets(userId, params)
}

func (s *JMService) SearchUserPermAssets(userId string, searchParams map[string]string) (assets []model.PermAsset, err error) {
	params := map[string]string{
		"limit": "100",
	}
	for k, v := range searchParams {
		params[k] = v
	}
	var ret model.PaginationResponse
	assets = make([]model.PermAsset, 0, 100)
	reqUrl := fmt.Sprintf(UserPermsAssetsURL, userId)
	_, err = s.authClient.Get(reqUrl, &ret, params)
	if err != nil {
		return
	}
	assets = append(assets, ret.Data...)
	for ret.NextURL != "" {
		ret, err = s.GetNextURLPermAssets(ret.NextURL)
		if err != nil {
			return
		}
		assets = append(assets, ret.Data...)
	}
	return
}

func (s *JMService) getPaginationAssets(reqUrl string, param model.PaginationParam) (resp model.PaginationResponse, err error) {
	var (
		loadAllAsset bool
		pageSize     int
	)
	pageSize = param.PageSize
	if param.PageSize <= 0 {
		loadAllAsset = true
		pageSize = 100
	}
	searches := make([]string, 0, len(param.Searches))
	for i := 0; i < len(param.Searches); i++ {
		searches = append(searches, strings.TrimSpace(param.Searches[i]))
	}
	paramsArray := make([]map[string]string, 0, len(param.Searches)+2)
	params := map[string]string{
		"limit":  strconv.Itoa(pageSize),
		"offset": strconv.Itoa(param.Offset),
	}
	if param.Refresh {
		params["rebuild_tree"] = "1"
	}
	if param.Order != "" {
		params["order"] = param.Order
	}
	if param.Type != "" {
		params["type"] = param.Type
	}
	if param.Category != "" {
		params["category"] = param.Category
	}

	if param.IsActive {
		params["is_active"] = "true"
	}
	if param.Protocols != nil {
		params["protocols"] = strings.Join(param.Protocols, ",")
	}
	if len(searches) > 0 {
		params["search"] = strings.Join(searches, ",")
	}

	paramsArray = append(paramsArray, params)

	if !loadAllAsset {
		_, err = s.authClient.Get(reqUrl, &resp, paramsArray...)
		return
	}
	var allData []model.PermAsset
	var pageResp model.PaginationResponse
	_, err = s.authClient.Get(reqUrl, &pageResp, paramsArray...)
	if err != nil {
		return pageResp, err
	}
	allData = append(allData, pageResp.Data...)
	for pageResp.NextURL != "" {
		pageResp, err = s.GetNextURLPermAssets(pageResp.NextURL)
		if err != nil {
			return
		}
		allData = append(allData, pageResp.Data...)
	}
	resp.Data = allData
	resp.Total = len(allData)
	return
}

func (s *JMService) GetNextURLPermAssets(reqUrl string) (resp model.PaginationResponse, err error) {
	result := TrimHost(reqUrl)
	_, err = s.authClient.Get(result, &resp)
	return
}

func TrimHost(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return raw // 解析失败就原样返回
		}
		path := u.Path
		result := path
		if u.RawQuery != "" {
			result += "?" + u.RawQuery
		}
		return result
	}
	return raw
}
