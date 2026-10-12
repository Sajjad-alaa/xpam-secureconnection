package service

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/logger"
	"github.com/jumpserver-dev/sdk-go/model"
)

func ScanRemainReplays(apiClient *JMService, replayDir string) map[string]model.RemainReplay {
	allRemainReplays := make(map[string]model.RemainReplay)
	_ = filepath.Walk(replayDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		var (
			sid        string
			targetDate string
			version    model.ReplayVersion
			ok         bool
		)

		filename := info.Name()
		if sid, ok = ParseReplaySessionID(filename); !ok {
			return nil
		}
		version = model.ParseReplayVersion(filename, model.Version2)
		finishedTime := common.NewUTCTime(info.ModTime())
		finishedSession, err2 := apiClient.SessionFinished(sid, finishedTime)
		if err2 != nil {
			logger.Errorf("Uploader service  mark session %s finished failed: %s", sid, err2)
			return nil
		}
		targetDate = finishedSession.DateStart.UTC().Format("2006-01-02")
		allRemainReplays[path] = model.RemainReplay{
			Id:          sid,
			Version:     version,
			TargetDate:  targetDate,
			AbsFilePath: path,
			IsGzip:      IsGzipFile(filename),
		}
		return nil
	})
	return allRemainReplays
}

func ParseReplaySessionID(filename string) (string, bool) {
	if len(filename) == 36 && common.IsUUID(filename) {
		return filename, true
	}
	sid := strings.Split(filename, ".")[0]
	if !common.IsUUID(sid) {
		return "", false
	}
	return sid, true
}

func IsGzipFile(src string) bool {
	return strings.HasSuffix(src, model.SuffixGz)
}
