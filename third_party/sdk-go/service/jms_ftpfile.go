package service

import "fmt"

func (s *JMService) UploadFTPFile(fid, file string) error {
	var res map[string]interface{}
	url := fmt.Sprintf(FTPLogFileURL, fid)
	return s.authClient.PostFileWithFields(url, file, nil, &res)
}

func (s *JMService) FinishFTPFile(fid string) error {
	data := map[string]bool{"has_file": true}
	url := fmt.Sprintf(FTPLogUpdateURL, fid)
	_, err := s.authClient.Patch(url, data, nil)
	return err
}
