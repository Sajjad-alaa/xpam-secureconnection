package service

import (
	"fmt"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
)

func (s *JMService) Upload(sessionID, gZipFile string) error {
	version := model.ParseReplayVersion(gZipFile, model.Version3)
	return s.UploadReplay(sessionID, gZipFile, version)
}

func (s *JMService) UploadReplay(sid, gZipFile string, version model.ReplayVersion) error {
	var res map[string]interface{}
	Url := fmt.Sprintf(SessionReplayURL, sid)
	fields := make(map[string]string)
	fields["version"] = string(version)
	return s.authClient.PostFileWithFields(Url, gZipFile, fields, &res)
}

func (s *JMService) FinishReply(sid string) (model.Session, error) {
	data := map[string]bool{"has_replay": true}
	return s.sessionPatch(sid, data)
}

func (s *JMService) FinishReplyWithSize(sid string, size int64) (model.Session, error) {
	data := map[string]interface{}{
		"has_replay":  true,
		"replay_size": size,
	}
	return s.sessionPatch(sid, data)
}

func (s *JMService) CreateSession(sess model.Session) (ret model.Session, err error) {
	_, err = s.authClient.Post(SessionListURL, sess, &ret)
	return
}

func (s *JMService) SessionSuccess(sid string) (model.Session, error) {
	data := map[string]bool{
		"is_success": true,
	}
	return s.sessionPatch(sid, data)
}

func (s *JMService) SessionFailed(sid string, err error) (model.Session, error) {
	data := map[string]interface{}{
		"is_success":   false,
		"error_reason": model.SessionReplayErrConnectFailed,
	}
	return s.sessionPatch(sid, data)
}

func (s *JMService) SessionReplayFailed(sid string, err model.ReplayError) (model.Session, error) {
	data := map[string]interface{}{
		"error_reason": err,
	}
	return s.sessionPatch(sid, data)
}

func (s *JMService) SessionDisconnect(sid string) (model.Session, error) {
	return s.SessionFinished(sid, common.NewNowUTCTime())
}

func (s *JMService) SessionFinished(sid string, time common.UTCTime) (resp model.Session, err error) {
	data := map[string]interface{}{
		"is_finished": true,
		"date_end":    time,
	}
	return s.sessionPatch(sid, data)
}

func (s *JMService) sessionPatch(sid string, data interface{}) (resp model.Session, err error) {
	Url := fmt.Sprintf(SessionDetailURL, sid)
	_, err = s.authClient.Patch(Url, data, &resp)
	return
}

func (s *JMService) GetSessionById(sid string) (data model.Session, err error) {
	reqURL := fmt.Sprintf(SessionDetailURL, sid)
	_, err = s.authClient.Get(reqURL, &data)
	return
}

func (s *JMService) CreateSessionTicketRelation(sid, ticketId string) (err error) {
	data := map[string]string{
		"session": sid,
		"ticket":  ticketId,
	}
	_, err = s.authClient.Post(TicketSessionURL, data, nil)
	return
}
