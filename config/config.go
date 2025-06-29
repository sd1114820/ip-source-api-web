package config

import "time"

// App 保存应用程序配置。
// All values are hardcoded.
var App = struct {
	// DataDir 是存储数据库文件的目录。
	DataDir string

	// UpdateInterval 是检查数据库更新的间隔（小时）。
	UpdateInterval int

	// ListenAddr 是服务器监听地址。
	ListenAddr string

	// ReadTimeout 是 HTTP 读取超时时间。
	ReadTimeout time.Duration

	// WriteTimeout 是 HTTP 写入超时时间。
	WriteTimeout time.Duration

	// IdleTimeout 是 HTTP 空闲超时时间。
	IdleTimeout time.Duration

	// CityDBName 是 GeoLite2 城市数据库的文件名。
	CityDBName string

	// AsnDBName 是 GeoLite2 ASN 数据库的文件名。
	AsnDBName string

	// CnDBName 是中国 IP 数据库的文件名。
	CnDBName string

	// MaxMindLicenseKey 是您的 MaxMind 许可证密钥。
	MaxMindLicenseKey string
}{
	DataDir:        "data",
	UpdateInterval: 24, // hours
	ListenAddr:     "0.0.0.0:8180",
	ReadTimeout:    5 * time.Second,
	WriteTimeout:   10 * time.Second,
	IdleTimeout:    120 * time.Second,
	CityDBName:     "GeoLite2-City.mmdb",
	AsnDBName:      "GeoLite2-ASN.mmdb",
	CnDBName:       "GeoCN.mmdb",

	MaxMindLicenseKey: "", // 请在环境变量中设置 MAXMIND_LICENSE_KEY
}
