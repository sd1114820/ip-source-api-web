package api

// Response 是 API 响应的结构，兼容 ip-api.com
// 扩展以包含 GeoCN 数据库字段
type Response struct {
	IP                 string  `json:"ip"`                     // IP 地址
	Network            string  `json:"network,omitempty"`      // 网络
	Version            string  `json:"version,omitempty"`      // 版本
	City               string  `json:"city"`                   // 城市名称
	CityCode           uint    `json:"city_code,omitempty"` // GeoCN city code
	Region             string  `json:"region"`                 // 地区/州
	RegionCode         string  `json:"region_code"`            // 地区代码
	ProvinceCode       uint    `json:"province_code,omitempty"`  // GeoCN province code
	Districts          string  `json:"districts,omitempty"`      // GeoCN districts
	DistrictsCode      uint    `json:"districts_code,omitempty"` // GeoCN districts code
	Country            string  `json:"country,omitempty"`      // 国家名称
	CountryName        string  `json:"country_name,omitempty"`  // 国家名称
	CountryCode        string  `json:"country_code,omitempty"`  // ISO 3166-1 alpha-2 国家代码
	CountryCodeISO3    string  `json:"country_code_iso3,omitempty"`
	CountryCapital     string  `json:"country_capital,omitempty"`
	CountryTLD         string  `json:"country_tld,omitempty"`
	ContinentCode      string  `json:"continent_code,omitempty"`
	InEU               bool    `json:"in_eu"`
	Postal             string  `json:"postal"`                 // 邮政编码
	Latitude           float64 `json:"latitude,omitempty"`     // 纬度
	Longitude          float64 `json:"longitude,omitempty"`    // 经度
	Timezone           string  `json:"timezone,omitempty"`
	UTCOffset          string  `json:"utc_offset,omitempty"`
	CountryCallingCode string  `json:"country_calling_code,omitempty"`
	Currency           string  `json:"currency,omitempty"`
	CurrencyName       string  `json:"currency_name,omitempty"`
	Languages          string  `json:"languages,omitempty"`
	CountryArea        float64 `json:"country_area,omitempty"`
	CountryPopulation  int64   `json:"country_population,omitempty"`
	ASN                string  `json:"asn,omitempty"`
	Org                string  `json:"org,omitempty"`
	ISP                string  `json:"isp,omitempty"`     // GeoCN ISP
	Message            string  `json:"message,omitempty"`      // 用于错误信息
}
