package geoip

import (
	"errors"
	"log"
	"net"
	"sync"

	"github.com/oschwald/geoip2-golang"
	"github.com/oschwald/maxminddb-golang"
)

var (
	cityDB  *geoip2.Reader
	asnDB   *geoip2.Reader
	cnRawDB *maxminddb.Reader // Raw maxminddb reader for GeoCN
	dbMux   sync.RWMutex
)

// OpenDBs 打开 GeoLite2 City、ASN 和 CN 数据库
func OpenDBs(cityDBPath, asnDBPath, cnDBPath string) error {
	dbMux.Lock()
	defer dbMux.Unlock()
	return openDBs(cityDBPath, asnDBPath, cnDBPath)
}

func openDBs(cityDBPath, asnDBPath, cnDBPath string) error {
	var err error
	cityDB, err = geoip2.Open(cityDBPath)
	if err != nil {
		log.Printf("Error opening GeoLite2-City database: %v", err)
		return err
	}
	log.Println("GeoLite2-City database opened successfully.")

	asnDB, err = geoip2.Open(asnDBPath)
	if err != nil {
		log.Printf("Error opening GeoLite2-ASN database: %v", err)
		return err
	}
	log.Println("GeoLite2-ASN database opened successfully.")

	cnRawDB, err = maxminddb.Open(cnDBPath)
	if err != nil {
		log.Printf("Error opening GeoCN raw database: %v", err)
	} else {
		log.Println("GeoCN raw database opened successfully.")
	}

	return nil
}

// CloseDBs 关闭数据库读取器
func CloseDBs() {
	dbMux.Lock()
	defer dbMux.Unlock()
	closeDBs()
}

func closeDBs() {
	if cityDB != nil {
		cityDB.Close()
		log.Println("GeoLite2-City database closed.")
	}
	if asnDB != nil {
		asnDB.Close()
		log.Println("GeoLite2-ASN database closed.")
	}

	if cnRawDB != nil {
		cnRawDB.Close()
		log.Println("GeoCN raw database closed.")
	}
}

// ReloadDBs 关闭并重新打开数据库以原子性地获取文件更改
func ReloadDBs(cityDBPath, asnDBPath, cnDBPath string) error {
	dbMux.Lock()
	defer dbMux.Unlock()

	log.Println("Reloading databases...")

	// 首先关闭现有数据库
	if cityDB != nil {
		cityDB.Close()
	}
	if asnDB != nil {
		asnDB.Close()
	}

	// 重新打开数据库
	err := openDBs(cityDBPath, asnDBPath, cnDBPath)
	if err != nil {
		return err
	}

	log.Println("Databases reloaded successfully.")
	return nil
}

// Lookup 对给定的 IP 地址执行查找
// 返回城市数据、ASN 数据、GeoCN 数据（如果可用）和错误
func Lookup(ip net.IP) (*geoip2.City, *geoip2.ASN, *GeoCNResult, error) {
	dbMux.RLock()
	defer dbMux.RUnlock()

	var city *geoip2.City
	var cnResult *GeoCNResult
	var err error

	// 默认使用 GeoLite2-City
	if cityDB != nil {
		city, err = cityDB.City(ip)
		if err != nil {
			// 记录错误但不立即返回，ASN 查找可能仍然有效
			log.Printf("GeoLite2-City lookup for %s failed: %v", ip, err)
		}
	}

	// 对于中国 IP，如果国家是 CN 或城市数据为空，则检查 GeoCN 数据库
	log.Printf("Checking GeoCN for IP %s: cnRawDB=%v, city=%v, country=%s", ip, cnRawDB != nil, city != nil, func() string {
		if city != nil {
			return city.Country.IsoCode
		}
		return "unknown"
	}())

	if cnRawDB != nil && (city == nil || city.Country.IsoCode == "CN") {
		log.Printf("Attempting GeoCN lookup for IP: %s", ip)
		cnResult, cnErr := queryGeoCNDatabase(ip)
		if cnErr == nil && cnResult != nil {
			log.Printf("GeoCN lookup successful for IP: %s, data: %+v", ip, cnResult)
			if city == nil {
				city = convertGeoCNToGeoLite2(cnResult)
				log.Printf("Using GeoCN as primary data source for %s", ip)
			} else {
				mergeGeoCNData(city, cnResult)
				log.Printf("Merged GeoCN data with GeoLite2 for %s", ip)
			}
		} else {
			log.Printf("GeoCN lookup failed for %s: %v", ip, cnErr)
			cnResult = nil // Ensure cnResult is nil on failure
		}
	} else {
		log.Printf("Skipping GeoCN lookup for IP %s: cnRawDB=%v", ip, cnRawDB != nil)
		cnResult = nil
	}

	// 如果 GeoCN 没有提供数据，则回退到 GeoLite2
	if city != nil && city.Country.IsoCode == "CN" {
		log.Printf("Chinese IP %s processed, checking data completeness", ip)
		if len(city.City.Names) == 0 {
			log.Printf("No city data available for Chinese IP %s", ip)
		}
		if len(city.Subdivisions) == 0 {
			log.Printf("No subdivision data available for Chinese IP %s", ip)
		}
	}

	var asn *geoip2.ASN
	if asnDB != nil {
		asn, _ = asnDB.ASN(ip) // Ignore error for ASN, as it's less critical
	}

	if cityDB == nil && asnDB == nil {
		return nil, nil, nil, errors.New("no GeoIP databases are available")
	}

	return city, asn, cnResult, nil // Return city, ASN, GeoCN data, and error
}

// GeoCNResult 表示 GeoCN 数据库响应的结构
type GeoCNResult struct {
	City          string `maxminddb:"city"`
	CityCode      uint   `maxminddb:"cityCode"`
	Districts     string `maxminddb:"districts"`
	DistrictsCode uint   `maxminddb:"districtsCode"`
	ISP           string `maxminddb:"isp"`
	Net           string `maxminddb:"net"`
	Province      string `maxminddb:"province"`
	ProvinceCode  uint   `maxminddb:"provinceCode"`
}

// queryGeoCNDatabase 使用 maxminddb 直接查询 GeoCN 数据库
func queryGeoCNDatabase(ip net.IP) (*GeoCNResult, error) {
	if cnRawDB == nil {
		return nil, errors.New("GeoCN raw database not available")
	}

	var result GeoCNResult
	err := cnRawDB.Lookup(ip, &result)
	if err != nil {
		return nil, err
	}

	// 检查是否获得有效数据（对于中国 IP，至少应该有省份信息）
	if result.Province == "" {
		return nil, errors.New("no GeoCN data found for IP")
	}

	log.Printf("GeoCN raw query result for %s: City=%s, Province=%s, Districts=%s, ISP=%s",
		ip.String(), result.City, result.Province, result.Districts, result.ISP)
	return &result, nil
}

// convertGeoCNToGeoLite2 将 GeoCN 结果转换为 GeoLite2 City 格式
func convertGeoCNToGeoLite2(cnResult *GeoCNResult) *geoip2.City {
	city := &geoip2.City{}

	// 设置国家信息
	city.Country.IsoCode = "CN"
	city.Country.Names = map[string]string{
		"en":    "China",
		"zh-CN": "中国",
	}

	// 设置城市信息 - 直接使用 GeoCN 城市字段
	if cnResult.City != "" {
		city.City.Names = map[string]string{
			"zh-CN": cnResult.City,
			"en":    cnResult.City, // Use Chinese name as fallback
		}
	}

	// 设置行政区划（省份）信息 - 使用匿名结构体类型
	if cnResult.Province != "" {
		city.Subdivisions = []struct {
			Names     map[string]string `maxminddb:"names"`
			IsoCode   string            `maxminddb:"iso_code"`
			GeoNameID uint              `maxminddb:"geoname_id"`
		}{
			{
				Names: map[string]string{
					"zh-CN": cnResult.Province,
				},
				IsoCode:   "",
				GeoNameID: 0,
			},
		}
	}

	// 设置位置时区（中国默认时区）
	city.Location.TimeZone = "Asia/Shanghai"

	log.Printf("Converted GeoCN data: City=%s, Province=%s, Districts=%s, ISP=%s",
		cnResult.City, cnResult.Province, cnResult.Districts, cnResult.ISP)
	return city
}

// mergeGeoCNData 将 GeoCN 数据合并到现有的 GeoLite2 城市数据中
func mergeGeoCNData(city *geoip2.City, cnResult *GeoCNResult) {
	// 如果可用，优先使用 GeoCN 数据 - 直接映射城市字段
	if cnResult.City != "" {
		if city.City.Names == nil {
			city.City.Names = make(map[string]string)
		}
		city.City.Names["zh-CN"] = cnResult.City
		// 为保持一致性，也将英文名称设置为中文
		city.City.Names["en"] = cnResult.City
		log.Printf("Updated city from GeoCN: %s", cnResult.City)
	}

	// 将 GeoCN 省份映射到 GeoLite2 行政区划（地区）- 使用匿名结构体类型
	if cnResult.Province != "" {
		if len(city.Subdivisions) == 0 {
			city.Subdivisions = []struct {
				Names     map[string]string `maxminddb:"names"`
				IsoCode   string            `maxminddb:"iso_code"`
				GeoNameID uint              `maxminddb:"geoname_id"`
			}{
				{
					Names:     make(map[string]string),
					IsoCode:   "",
					GeoNameID: 0,
				},
			}
		}
		if city.Subdivisions[0].Names == nil {
			city.Subdivisions[0].Names = make(map[string]string)
		}
		// 将省份映射到地区
		city.Subdivisions[0].Names["zh-CN"] = cnResult.Province
		city.Subdivisions[0].Names["en"] = cnResult.Province
		log.Printf("Updated province/region from GeoCN: %s", cnResult.Province)
	}

	// 确保国家设置为中国
	if city.Country.IsoCode != "CN" {
		city.Country.IsoCode = "CN"
		city.Country.Names = map[string]string{"en": "China", "zh-CN": "中国"}
	}

	// 确保为中国 IP 设置时区
	if city.Location.TimeZone == "" {
		city.Location.TimeZone = "Asia/Shanghai"
	}

	// 记录额外的 GeoCN 数据用于调试
	if cnResult.Districts != "" {
		log.Printf("GeoCN Districts: %s", cnResult.Districts)
	}
	if cnResult.ISP != "" {
		log.Printf("GeoCN ISP: %s", cnResult.ISP)
	}
}

// OpenTestDB 用于测试MMDB文件是否有效，返回一个可以关闭的数据库连接
func OpenTestDB(dbPath string) (*geoip2.Reader, error) {
	return geoip2.Open(dbPath)
}
