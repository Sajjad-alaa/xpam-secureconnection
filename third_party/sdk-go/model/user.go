package model

import (
	"fmt"

	"github.com/jumpserver-dev/sdk-go/common"
)

type User struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Username         string `json:"username"`
	Email            string `json:"email"`
	Role             string `json:"role"`
	IsValid          bool   `json:"is_valid"`
	IsActive         bool   `json:"is_active"`
	OTPLevel         int    `json:"otp_level"`
	Language         string `json:"lang"`
	HasPublicKeys    bool   `json:"has_public_keys"`
	IsServiceAccount bool   `json:"is_service_account"`

	OrgRoles    []OrgRole    `json:"org_roles"`
	SystemRoles []SystemRole `json:"system_roles"`

	IsOrgAdmin  bool            `json:"is_org_admin"`
	IsSuperuser bool            `json:"is_superuser"`
	IsExpired   bool            `json:"is_expired"`
	Groups      []Group         `json:"groups"`
	DateExpired *common.UTCTime `json:"date_expired"`
	LastLogin   *common.UTCTime `json:"last_login"`
	Phone       Phone           `json:"phone"`
}

type MiniUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

func (u *User) String() string {
	return fmt.Sprintf("%s(%s)", u.Name, u.Username)
}

type UserKokoPreference struct {
	Basic KokoBasic `json:"basic"`
}
type KokoBasic struct {
	ThemeName string `json:"terminal_theme_name"`
}

type OrgRole struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type SystemRole struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Phone struct {
	CountryCode string `json:"code"`
	Number      string `json:"phone"`
}
