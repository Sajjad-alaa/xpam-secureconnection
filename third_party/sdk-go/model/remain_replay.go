package model

import "strings"

type RemainReplay struct {
	Id          string // session id
	TargetDate  string
	AbsFilePath string
	Version     ReplayVersion
	IsGzip      bool
}

func (r *RemainReplay) TargetPath() string {
	gzFilename := r.GetGzFilename()
	return strings.Join([]string{r.TargetDate, gzFilename}, "/")
}

func (r *RemainReplay) GetGzFilename() string {
	suffixGz := ".replay.gz"
	switch r.Version {
	case Version3:
		suffixGz = ".cast.gz"
	case Version2:
		suffixGz = ".replay.gz"
	}
	return r.Id + suffixGz
}
