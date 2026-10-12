package videoworker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/jumpserver-dev/sdk-go/httplib"
	"github.com/jumpserver-dev/sdk-go/model"
)

const (
	orgHeaderKey   = "X-JMS-ORG"
	orgHeaderValue = "ROOT"
)

func NewClient(baseUrl string, key model.AccessKey, Insecure bool) *WorkClient {
	opts := make([]httplib.Opt, 0, 2)
	if Insecure {
		opts = append(opts, httplib.WithInsecure())
	}
	client, err := httplib.NewClient(baseUrl, 30*time.Second, opts...)
	if err != nil {
		return nil
	}
	sign := ProfileAuth{
		KeyID:    key.ID,
		SecretID: key.Secret,
	}
	client.SetAuthSign(&sign)
	client.SetHeader(orgHeaderKey, orgHeaderValue)
	return &WorkClient{BaseURL: baseUrl, client: client}
}

type WorkClient struct {
	BaseURL string
	sign    httplib.AuthSign
	client  *httplib.Client

	cacheToken map[string]interface{}
}

func (w *WorkClient) CreateReplaySessionTask(sessionId, replayDirPath string, taskConfig *TaskConfig) (string, error) {
	// 读取 replay json
	replayMetaFilePath := path.Join(replayDirPath, fmt.Sprintf("%s.replay.json", sessionId))

	f, err := os.Open(replayMetaFilePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var replayMeta ReplayMeta
	if err := json.NewDecoder(f).Decode(&replayMeta); err != nil {
		return "", fmt.Errorf("error decoding replay meta file: %w", err)
	}
	// 这里计算文件的checksum
	for i, file := range replayMeta.Files {
		replayPath := filepath.Join(replayDirPath, file.Name)
		checksum, err := FileSHA256(replayPath)
		if err != nil {
			return "", fmt.Errorf("error calculating replay checksum: %w", err)
		}
		replayMeta.Files[i].Checksum = checksum
	}

	if taskConfig == nil {
		taskConfig = &TaskConfig{}
	}
	resp, err := w.CreateNewReplaySessionTask(sessionId, CreateReplayTaskRequest{
		Config: *taskConfig,
		Meta:   replayMeta,
	})
	if err != nil {
		return "", fmt.Errorf("error creating replay session task: %w", err)
	}

	taskId := resp.ID

	for i := range replayMeta.Files {
		fileMeta := replayMeta.Files[i]
		filePath := path.Join(replayDirPath, fileMeta.Name)

		resp, err := w.UploadReplayPart(taskId, i, filePath)
		if err != nil {
			return "", fmt.Errorf("error uploading replay part: %w", err)
		}
		if !resp.Ok {
			return "", fmt.Errorf("error upload replay part: %s", resp.ErrorMessage)
		}
	}

	return taskId, nil
}

type CreateReplayTaskRequest struct {
	Config TaskConfig `json:"config"`
	Meta   ReplayMeta `json:"meta"`
}

type CreateReplayTaskResponse struct {
	ID string `json:"id"`
}

func (w *WorkClient) CreateNewReplaySessionTask(sessionId string, req CreateReplayTaskRequest) (resp CreateReplayTaskResponse, err error) {
	reqUrl := fmt.Sprintf(ReplayFileURL, sessionId)
	_, err = w.client.Post(reqUrl, &req, &resp)
	return
}

type UploadReplayPartResponse struct {
	Ok           bool   `json:"ok"`
	ErrorMessage string `json:"error_message"`
}

func (w *WorkClient) UploadReplayPart(sessionId string, index int, partFilePath string) (resp UploadReplayPartResponse, err error) {
	reqUrl := fmt.Sprintf(ReplayUploadURL, sessionId, index)
	err = w.client.PostFileWithFields(reqUrl, partFilePath, map[string]string{}, &resp)
	return
}

func FileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

const (
	ReplayFileURL   = "/api/v2/replay/sessions/%s/task/"
	ReplayUploadURL = "/api/v2/replay/sessions/%s/task/upload/%d"
)
