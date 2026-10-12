package model

type AccountChat struct {
	Id         string       `json:"id"`
	Name       string       `json:"name"`
	Username   string       `json:"username"`
	SecretType LabelValue   `json:"secret_type"`
	SuFrom     BaseAccount  `json:"su_from"`
	Asset      AccountAsset `json:"asset"`
	Version    int          `json:"version"`
	Source     LabelValue   `json:"source"`
	OrgId      string       `json:"org_id"`
	OrgName    string       `json:"org_name"`
	Privileged bool         `json:"privileged"`
	IsActive   bool         `json:"is_active"`

	Comment string `json:"comment"`
}

type AccountAsset struct {
	Id       string       `json:"id"`
	Name     string       `json:"name"`
	Address  string       `json:"address"`
	Type     LabelValue   `json:"type"`
	Category LabelValue   `json:"category"`
	Platform BasePlatform `json:"platform"`
}
