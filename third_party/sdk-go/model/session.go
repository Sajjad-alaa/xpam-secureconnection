package model

import (
	"encoding/json"
	"strings"

	"github.com/jumpserver-dev/sdk-go/common"
)

var (
	_ json.Unmarshaler = (*LabelField)(nil)
	_ json.Marshaler   = (*LabelField)(nil)
)

type LabelField string

func (s LabelField) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s + `"`), nil
}

func (s *LabelField) UnmarshalJSON(bytes []byte) error {
	var labelValue struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(bytes, &labelValue); err == nil {
		*s = LabelField(labelValue.Value)
		return nil
	}
	*s = LabelField(strings.Trim(string(bytes), `"`))
	return nil
}

const (
	NORMALType  LabelField = "normal"
	TUNNELType  LabelField = "tunnel"
	COMMANDType LabelField = "command"
	SFTPType    LabelField = "sftp"
)

const (
	LoginFromSSH LabelField = "ST"
	LoginFromWeb LabelField = "WT"
	LoginFromRT  LabelField = "RT"
	LoginFromDT  LabelField = "DT"
	LoginFromVT  LabelField = "VT"
)

type Session struct {
	ID         string         `json:"id,omitempty"`
	User       string         `json:"user"`
	Asset      string         `json:"asset"`
	Account    string         `json:"account"`
	LoginFrom  LabelField     `json:"login_from,omitempty"`
	RemoteAddr string         `json:"remote_addr"`
	Protocol   string         `json:"protocol"`
	DateStart  common.UTCTime `json:"date_start"`
	OrgID      string         `json:"org_id"`
	UserID     string         `json:"user_id"`
	AssetID    string         `json:"asset_id"`
	AccountID  string         `json:"account_id"`
	Type       LabelField     `json:"type"`
	ErrReason  LabelField     `json:"error_reason,omitempty"`
	TokenId    string         `json:"token_id,omitempty"`
	Comment    string         `json:"comment,omitempty"`
	LangCode   string         `json:"lang_code,omitempty"`
}

type ReplayVersion string

const (
	UnKnown  ReplayVersion = ""
	Version2 ReplayVersion = "2"
	Version3 ReplayVersion = "3"
	Version4 ReplayVersion = "4"
	Version5 ReplayVersion = "5"
)

const (
	SuffixGz            = ".gz"
	SuffixReplayGz      = ".replay.gz"
	SuffixCastGz        = ".cast.gz"
	SuffixCast          = ".cast"
	SuffixGuac          = ".guac"
	SuffixPartReplay    = ".part.gz"
	SuffixReplayJson    = ".replay.json"
	SuffixReplayMP4     = ".replay.mp4"
	SuffixReplayPartMp4 = ".part.mp4"
)

var SuffixVersionMap = map[string]ReplayVersion{
	SuffixReplayPartMp4: Version5,
	SuffixPartReplay:    Version5,
	SuffixReplayJson:    Version5,
	SuffixReplayGz:      Version2,
	SuffixCastGz:        Version3,
	SuffixReplayMP4:     Version4,
}

func ParseReplayVersion(gzFile string, defaultValue ReplayVersion) ReplayVersion {
	for suffix, version := range SuffixVersionMap {
		if strings.HasSuffix(gzFile, suffix) {
			return version
		}
	}
	return defaultValue
}

type ReplayError LabelField

func (r ReplayError) Error() string {
	return string(r)
}

const (
	SessionReplayErrConnectFailed ReplayError = "connect_failed"
	SessionReplayErrCreatedFailed ReplayError = "replay_create_failed"
	SessionReplayErrUploadFailed  ReplayError = "replay_upload_failed"
	SessionReplayErrUnsupported   ReplayError = "replay_unsupported"
	SessionReplayErrConvertFailed ReplayError = "replay_convert_failed"
)

type LifecycleEvent string

const (
	AssetConnectSuccess  LifecycleEvent = "asset_connect_success"
	AssetConnectFinished LifecycleEvent = "asset_connect_finished"
	CreateShareLink      LifecycleEvent = "create_share_link"
	UserJoinSession      LifecycleEvent = "user_join_session"
	UserLeaveSession     LifecycleEvent = "user_leave_session"
	AdminJoinMonitor     LifecycleEvent = "admin_join_monitor"
	AdminExitMonitor     LifecycleEvent = "admin_exit_monitor"
	ReplayConvertStart   LifecycleEvent = "replay_convert_start"
	ReplayConvertSuccess LifecycleEvent = "replay_convert_success"
	ReplayConvertFailure LifecycleEvent = "replay_convert_failure"
	ReplayUploadStart    LifecycleEvent = "replay_upload_start"
	ReplayUploadSuccess  LifecycleEvent = "replay_upload_success"
	ReplayUploadFailure  LifecycleEvent = "replay_upload_failure"
)

type SessionLifecycleLog struct {
	Reason string `json:"reason"`
	User   string `json:"user"`
}

var EmptyLifecycleLog = SessionLifecycleLog{}

type SessionLifecycleReasonErr string

func (s SessionLifecycleReasonErr) String() string {
	return string(s)
}

const (
	ReasonErrConnectFailed     SessionLifecycleReasonErr = "connect_failed"
	ReasonErrConnectDisconnect SessionLifecycleReasonErr = "connect_disconnect"
	ReasonErrUserClose         SessionLifecycleReasonErr = "user_close"
	ReasonErrIdleDisconnect    SessionLifecycleReasonErr = "idle_disconnect"
	ReasonErrAdminTerminate    SessionLifecycleReasonErr = "admin_terminate"
	ReasonErrMaxSessionTimeout SessionLifecycleReasonErr = "max_session_timeout"
	ReasonErrPermissionExpired SessionLifecycleReasonErr = "permission_expired"
	ReasonErrNullStorage       SessionLifecycleReasonErr = "null_storage"
)
