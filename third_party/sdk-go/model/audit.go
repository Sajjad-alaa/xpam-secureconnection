package model

import (
	"strings"

	"github.com/jumpserver-dev/sdk-go/common"
)

type FTPLog struct {
	ID         string         `json:"id"`
	User       string         `json:"user"`
	Asset      string         `json:"asset"`
	OrgID      string         `json:"org_id"`
	Account    string         `json:"account"`
	RemoteAddr string         `json:"remote_addr"`
	Operate    string         `json:"operate"`
	Path       string         `json:"filename"`
	DateStart  common.UTCTime `json:"date_start"`
	IsSuccess  bool           `json:"is_success"`
	Session    string         `json:"session"`
}

func (f *FTPLog) TargetPath() string {
	today := f.DateStart.UTC().Format(dateTimeFormat)
	return strings.Join([]string{FtpTargetPrefix, today, f.ID}, "/")
}

const (
	FtpTargetPrefix = "FTP_FILES"
	dateTimeFormat  = "2006-01-02"
)

const (
	OperateDownload = "download"
	OperateUpload   = "upload"
)

const (
	OperateRemoveDir = "rmdir"
	OperateRename    = "rename"
	OperateRenameDir = "rename_dir"
	OperateMkdir     = "mkdir"
	OperateDelete    = "delete"
	OperateSymlink   = "symlink"
)
