package srvconn

import (
	"sync"

	"xpam/sdk-go/common"
	"xpam/sdk-go/model"
	"xpam/sdk-go/service"
	"xpam/secureconnection/pkg/logger"
	"xpam/secureconnection/pkg/session"
)

type SftpSession struct {
	*SftpConn
	sess       *model.Session
	once       sync.Once
	jmsService *service.JMService
	onClosed   func()
}

func (s *SftpSession) CloseWithReason(reason model.SessionLifecycleReasonErr) {
	s.once.Do(func() {
		s.SftpConn.Close()
		session.RemoveSessionById(s.sess.ID)
		if _, err := s.jmsService.SessionFinished(s.sess.ID, common.NewNowUTCTime()); err != nil {
			logger.Errorf("SFTP Session finished err: %s", err)
		}
		logger.Debugf("SFTP Session finished %s", s.sess.ID)
		logObj := model.SessionLifecycleLog{Reason: reason.String()}
		if err := s.jmsService.RecordSessionLifecycleLog(s.sess.ID, model.AssetConnectFinished, logObj); err != nil {
			logger.Errorf("Update session %s lifecycle asset_connect_finished failed: %s", s.sess.ID, err)
		}
		if s.onClosed != nil {
			s.onClosed()
		}
	})

}

func (s *SftpSession) Close() {
	s.CloseWithReason(model.ReasonErrConnectDisconnect)
}
