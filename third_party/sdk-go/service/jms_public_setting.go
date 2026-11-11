package service

import "xpam/sdk-go/model"

func (s *JMService) GetPublicSetting() (result model.PublicSetting, err error) {
	_, err = s.authClient.Get(PublicSettingURL, &result)
	return
}
