package config

import (
	"fmt"
	"os"

	"github.com/jumpserver-dev/sdk-go/common"
)

type CommonConfig struct {
	Name           string `mapstructure:"NAME"`
	CoreHost       string `mapstructure:"CORE_HOST"`
	BootstrapToken string `mapstructure:"BOOTSTRAP_TOKEN"`
	LanguageCode   string `mapstructure:"LANGUAGE_CODE"`

	IgnoreVerifyCerts bool `mapstructure:"IGNORE_VERIFY_CERTS"`

	LogLevel string `mapstructure:"LOG_LEVEL"`

	ShareRoomType string   `mapstructure:"SHARE_ROOM_TYPE"`
	RedisHost     string   `mapstructure:"REDIS_HOST"`
	RedisPort     int      `mapstructure:"REDIS_PORT"`
	RedisPassword string   `mapstructure:"REDIS_PASSWORD"`
	RedisDBIndex  int      `mapstructure:"REDIS_DB_ROOM"`
	RedisClusters []string `mapstructure:"REDIS_CLUSTERS"`

	RedisSentinelPassword string `mapstructure:"REDIS_SENTINEL_PASSWORD"`
	RedisSentinelHosts    string `mapstructure:"REDIS_SENTINEL_HOSTS"`
	RedisUseSSL           bool   `mapstructure:"REDIS_USE_SSL"`
}

const (
	hostEnvKey = "SERVER_HOSTNAME"

	defaultNameMaxLen = 128
)

/*
SERVER_HOSTNAME: 环境变量名，可用于自定义默认注册名称的前缀
default name rule:
prefixName-{SERVER_HOSTNAME}-{HOSTNAME}-RandomStr
 or
prefixName-{HOSTNAME}-RandomStr
*/

func GetDefaultName(prefixName string) string {
	hostname, _ := os.Hostname()
	hostname = fmt.Sprintf("%s-%s", hostname, common.RandomStr(7))
	if serverHostname, ok := os.LookupEnv(hostEnvKey); ok {
		hostname = fmt.Sprintf("%s-%s", serverHostname, hostname)
	}
	hostRune := []rune(prefixName + hostname)
	if len(hostRune) <= defaultNameMaxLen {
		return string(hostRune)
	}
	name := make([]rune, defaultNameMaxLen)
	index := defaultNameMaxLen / 2
	copy(name[:index], hostRune[:index])
	start := len(hostRune) - index
	copy(name[index:], hostRune[start:])
	return string(name)
}
