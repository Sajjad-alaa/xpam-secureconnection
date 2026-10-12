package videoworker

type TaskConfig struct {
	MaxFrame int `form:"max_frame"`
	Width    int `form:"width"`
	Height   int `form:"height"`
	Bitrate  int `form:"bitrate"`
}
type ReplayMeta struct {
	ID         string     `json:"id"`
	User       string     `json:"user"`
	Asset      string     `json:"asset"`
	Account    string     `json:"account"`
	LoginFrom  string     `json:"login_from"`
	RemoteAddr string     `json:"remote_addr"`
	Protocol   string     `json:"protocol"`
	OrgId      string     `json:"org_id"`
	UserId     string     `json:"user_id"`
	AssetId    string     `json:"asset_id"`
	AccountId  string     `json:"account_id"`
	DateStart  string     `json:"date_start"`
	DateEnd    string     `json:"date_end"`
	Type       string     `json:"type"`
	Files      []FileMeta `json:"files"`
}

type FileMeta struct {
	Name     string `json:"name"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Duration int64  `json:"duration"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum,omitempty"`
}
