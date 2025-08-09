package api

import (
	"encoding/json"
	"fmt"
	"io"
	"ip-api/config"
	"ip-api/geoip"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/patrickmn/go-cache"
)

// 创建一个默认过期时间为 5 分钟的缓存，
// 每 10 分钟清除过期项目
var ipCache = cache.New(5*time.Minute, 10*time.Minute)

// IPHandler 处理 IP 查找请求
func IPHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	ipStr := getIPFromRequest(r)

	ip := net.ParseIP(ipStr)
	if ip == nil {
		log.Printf("Invalid IP address provided: %s", ipStr)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Response{
			IP:      ipStr,
			Message: "invalid query",
		})
		return
	}

	// 首先检查缓存
	fieldsStr := r.URL.Query().Get("fields")
	cacheKey := ip.String() + "?fields=" + fieldsStr

	if cachedResponse, found := ipCache.Get(cacheKey); found {
		log.Printf("Serving IP %s from cache", ip.String())
		w.Header().Set("X-Cache", "HIT")
		if err := json.NewEncoder(w).Encode(cachedResponse); err != nil {
			log.Printf("JSON encode error for cached response: %v", err)
		}
		return
	}

	log.Printf("Looking up IP: %s", ip.String())
	city, asn, cnResult, err := geoip.Lookup(ip)
	if err != nil {
		// 首先检查内部错误（例如，数据库未打开、文件损坏）
		if !strings.Contains(err.Error(), "is not in the database") {
			log.Printf("Internal server error during lookup for IP %s: %v", ip.String(), err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(Response{
				IP:      ipStr,
				Message: "internal error",
			})
			return
		}

		// 处理客户端错误（例如，私有/保留 IP、不在数据库中）
		log.Printf("Failed to lookup IP %s: %v", ip.String(), err)
		message := "invalid query" // 默认消息
		if ip.IsPrivate() {
			message = "private range"
		} else if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			message = "reserved range"
		} else if strings.Contains(err.Error(), "is not in the database") {
			message = "not in database"
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(Response{
			IP:      ipStr,
			Message: message,
		})
		return
	}

	// 构建完整的响应结构
	fullResp := buildSuccessResponse(ip, city, asn, cnResult)

	var finalResp interface{}
	if fieldsStr != "" {
		finalResp = filterResponse(fullResp, fieldsStr)
	} else {
		finalResp = fullResp
	}

	ipCache.Set(cacheKey, finalResp, cache.DefaultExpiration)
	json.NewEncoder(w).Encode(finalResp)

	log.Printf("Successfully served lookup for IP: %s", ip.String())
}

// getIPFromRequest extracts the IP address string from the HTTP request.
func getIPFromRequest(r *http.Request) string {
	// 1. 从 URL 路径获取
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) > 1 && parts[0] == "json" {
		return parts[1]
	}

	// 2. 从 "query" URL 参数获取
	if q := r.URL.Query().Get("query"); q != "" {
		return q
	}

	// 3. 从 X-Forwarded-For 头部获取
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// 4. 从 X-Real-IP 头部获取
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// 5. 回退到 RemoteAddr
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// buildSuccessResponse 从查找结果创建 Response 结构
// 扩展以包含 GeoCN 数据，提供全面的中国 IP 信息
func buildSuccessResponse(ip net.IP, city *geoip2.City, asn *geoip2.ASN, cnResult *geoip.GeoCNResult) Response {
	resp := Response{
		IP: ip.String(),
	}

	// 网络和版本信息
	// 注意：geoip2-golang v1 中的 ASN 结构没有 Network 字段
	// 我们将基于 IP 类别生成基本的网络范围
	if ip.To4() != nil {
		resp.Version = "IPv4"
		// 为 IPv4 生成基本的 /24 网络
		ipv4 := ip.To4()
		resp.Network = fmt.Sprintf("%d.%d.%d.0/24", ipv4[0], ipv4[1], ipv4[2])
	} else {
		resp.Version = "IPv6"
		// 为 IPv6 生成基本的 /64 网络
		resp.Network = fmt.Sprintf("%s/64", ip.String()[:19])
	}

	if city != nil {
		// 国家信息
		resp.Country = city.Country.IsoCode
		resp.CountryCode = city.Country.IsoCode
		if len(city.Country.Names) > 0 {
			// 对于 CN 查找，优先使用中文名称
			if resp.CountryCode == "CN" && city.Country.Names["zh-CN"] != "" {
				resp.CountryName = city.Country.Names["zh-CN"]
			} else {
				resp.CountryName = city.Country.Names["en"]
			}
		}
		resp.CountryCodeISO3 = getISO3Code(city.Country.IsoCode)
		resp.CountryTLD = getCountryTLD(city.Country.IsoCode)
		resp.CountryCallingCode = getCountryCallingCode(city.Country.IsoCode)
		resp.Currency = getCurrency(city.Country.IsoCode)
		resp.CurrencyName = getCurrencyName(city.Country.IsoCode)
		resp.Languages = getLanguages(city.Country.IsoCode)
		resp.CountryArea = getCountryArea(city.Country.IsoCode)
		resp.CountryPopulation = getCountryPopulation(city.Country.IsoCode)

		// 香港和台湾的特殊处理 - 更改国家名称、首都、地区和城市
		if city.Country.IsoCode == "HK" || city.Country.IsoCode == "TW" {
			resp.Country = "CN"
			resp.CountryCode = "CN"
			resp.CountryName = "中国"
			resp.CountryCapital = "北京"
			if city.Country.IsoCode == "HK" {
				resp.Region = "香港"
				resp.City = "香港"
			} else if city.Country.IsoCode == "TW" {
				resp.Region = "台湾"
				resp.City = "台湾"
			}
		} else {
			resp.CountryCapital = getCountryCapital(city.Country.IsoCode)
		}

		// 大洲信息
		resp.ContinentCode = city.Continent.Code
		resp.InEU = isEUCountry(city.Country.IsoCode)

		// 城市和地区信息
		log.Printf("GeoLite2 city data for %s: %v", ip.String(), city.City.Names)
		if city.City.Names != nil {
			if name, ok := city.City.Names["zh-CN"]; ok && name != "" {
				resp.City = name
			} else if name, ok := city.City.Names["en"]; ok && name != "" {
				resp.City = name
			}
		}

		log.Printf("GeoLite2 subdivision data for %s: %d subdivisions", ip.String(), len(city.Subdivisions))
		if len(city.Subdivisions) > 0 && city.Subdivisions[0].Names != nil {
			subdivision := city.Subdivisions[0]
			log.Printf("First subdivision names: %v, IsoCode: %s", subdivision.Names, subdivision.IsoCode)
			resp.RegionCode = subdivision.IsoCode
			if name, ok := subdivision.Names["zh-CN"]; ok && name != "" {
				resp.Region = name
			} else if name, ok := subdivision.Names["en"]; ok && name != "" {
				resp.Region = name
			}
		}

		// 如果城市为空的回退处理
		if resp.City == "" {
			if isCityState(resp.CountryCode) {
				resp.City = resp.CountryName
				log.Printf("Using country name as city for city-state: %s", resp.City)
			} else if resp.CountryCode == "CN" && resp.Region != "" {
				// 对于没有城市信息的中国 IP，使用地区作为城市
				resp.City = resp.Region
				log.Printf("Using subdivision as city for CN IP: %s", resp.City)
			}
		}

		// 位置信息
		resp.Postal = city.Postal.Code
		// 对于中国 IP，如果缺少邮政编码，尝试基于地区提供
		if resp.Postal == "" && resp.CountryCode == "CN" {
			resp.Postal = getChinesePostalCode(resp.RegionCode, resp.Region)
			if resp.Postal != "" {
				log.Printf("Inferred postal code for CN IP %s: %s", ip.String(), resp.Postal)
			}
		}
		resp.Latitude = city.Location.Latitude
		resp.Longitude = city.Location.Longitude
		resp.Timezone = city.Location.TimeZone
		resp.UTCOffset = getUTCOffset(city.Location.TimeZone)
	}

	// ASN 信息
	if asn != nil && asn.AutonomousSystemNumber > 0 {
		resp.ASN = fmt.Sprintf("AS%d", asn.AutonomousSystemNumber)
		resp.Org = asn.AutonomousSystemOrganization
	}

	// GeoCN 信息 - 对于中国 IP 优先使用 GeoCN 数据
	log.Printf("Processing GeoCN data: cnResult=%v", cnResult != nil)
	if cnResult != nil {
		log.Printf("GeoCN data available: %+v", cnResult)
		// 将 GeoCN 字段直接映射到响应
		if cnResult.City != "" {
			resp.City = cnResult.City
			log.Printf("Using GeoCN city: %s", cnResult.City)
		}
		if cnResult.Province != "" {
			resp.Region = cnResult.Province
			log.Printf("Using GeoCN province as region: %s", cnResult.Province)
		}
		// 添加 GeoCN 特定字段
		resp.CityCode = cnResult.CityCode
		resp.ProvinceCode = cnResult.ProvinceCode
		resp.Districts = cnResult.Districts
		resp.DistrictsCode = cnResult.DistrictsCode
		resp.ISP = cnResult.ISP

		log.Printf("GeoCN data mapped: City=%s, Province=%s, Districts=%s, ISP=%s",
			cnResult.City, cnResult.Province, cnResult.Districts, cnResult.ISP)
	}

	return resp
}

// filterResponse 创建一个仅包含从 Response 结构请求的字段的映射
func filterResponse(resp Response, fieldsStr string) map[string]interface{} {
	requestFields := make(map[string]struct{})
	for _, f := range strings.Split(fieldsStr, ",") {
		requestFields[strings.TrimSpace(f)] = struct{}{}
	}

	val := reflect.ValueOf(resp)
	typeOfT := val.Type()
	filteredMap := make(map[string]interface{})

	// 始终包含 ip
	filteredMap["ip"] = resp.IP

	for i := 0; i < val.NumField(); i++ {
		field := typeOfT.Field(i)
		jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]

		if _, ok := requestFields[jsonTag]; ok {
			valueField := val.Field(i)
			if !valueField.IsZero() || !strings.Contains(field.Tag.Get("json"), "omitempty") {
				filteredMap[jsonTag] = valueField.Interface()
			}
		}
	}

	return filteredMap
}

// 获取国家特定信息的辅助函数

// getISO3Code 返回给定 ISO2 代码的 ISO3 国家代码
func getISO3Code(iso2 string) string {
	countryISO3 := map[string]string{
		"US": "USA", "CN": "CHN", "JP": "JPN", "DE": "DEU", "GB": "GBR",
		"FR": "FRA", "IT": "ITA", "ES": "ESP", "CA": "CAN", "AU": "AUS",
		"BR": "BRA", "IN": "IND", "RU": "RUS", "KR": "KOR", "MX": "MEX",
		"NL": "NLD", "SE": "SWE", "NO": "NOR", "DK": "DNK", "FI": "FIN",
		"CH": "CHE", "AT": "AUT", "BE": "BEL", "IE": "IRL", "PT": "PRT",
		"GR": "GRC", "PL": "POL", "CZ": "CZE", "HU": "HUN", "SK": "SVK",
		"SI": "SVN", "HR": "HRV", "BG": "BGR", "RO": "ROU", "LT": "LTU",
		"LV": "LVA", "EE": "EST", "MT": "MLT", "CY": "CYP", "LU": "LUX",
		"HK": "HKG", "SG": "SGP", "TW": "TWN", "TH": "THA", "MY": "MYS",
		"ID": "IDN", "PH": "PHL", "VN": "VNM", "BD": "BGD", "PK": "PAK",
		"LK": "LKA", "NP": "NPL", "MM": "MMR", "KH": "KHM", "LA": "LAO",
		"MN": "MNG", "KZ": "KAZ", "UZ": "UZB", "KG": "KGZ", "TJ": "TJK",
		"TM": "TKM", "AF": "AFG", "IR": "IRN", "IQ": "IRQ", "SY": "SYR",
		"LB": "LBN", "JO": "JOR", "IL": "ISR", "PS": "PSE", "SA": "SAU",
		"AE": "ARE", "QA": "QAT", "BH": "BHR", "KW": "KWT", "OM": "OMN",
		"YE": "YEM", "EG": "EGY", "LY": "LBY", "TN": "TUN", "DZ": "DZA",
		"MA": "MAR", "SD": "SDN", "ET": "ETH", "KE": "KEN", "UG": "UGA",
		"TZ": "TZA", "RW": "RWA", "BI": "BDI", "DJ": "DJI", "SO": "SOM",
		"ER": "ERI", "SS": "SSD", "CF": "CAF", "TD": "TCD", "CM": "CMR",
		"NG": "NGA", "NE": "NER", "BF": "BFA", "ML": "MLI", "SN": "SEN",
		"MR": "MRT", "GW": "GNB", "GN": "GIN", "SL": "SLE", "LR": "LBR",
		"CI": "CIV", "GH": "GHA", "TG": "TGO", "BJ": "BEN", "GA": "GAB",
		"GQ": "GNQ", "ST": "STP", "AO": "AGO", "ZM": "ZMB", "ZW": "ZWE",
		"BW": "BWA", "NA": "NAM", "ZA": "ZAF", "LS": "LSO", "SZ": "SWZ",
		"MZ": "MOZ", "MW": "MWI", "MG": "MDG", "MU": "MUS", "SC": "SYC",
		"KM": "COM", "AR": "ARG", "CL": "CHL", "PE": "PER", "BO": "BOL",
		"PY": "PRY", "UY": "URY", "EC": "ECU", "CO": "COL", "VE": "VEN",
		"GY": "GUY", "SR": "SUR", "GF": "GUF", "FK": "FLK", "GS": "SGS",
		"NZ": "NZL", "FJ": "FJI", "PG": "PNG", "SB": "SLB", "VU": "VUT",
		"NC": "NCL", "PF": "PYF", "WS": "WSM", "TO": "TON", "TV": "TUV",
		"NR": "NRU", "KI": "KIR", "MH": "MHL", "FM": "FSM", "PW": "PLW",
	}
	if iso3, ok := countryISO3[iso2]; ok {
		return iso3
	}
	return ""
}

// getCountryCapital 返回给定国家代码的首都城市
func getCountryCapital(countryCode string) string {
	capitals := map[string]string{
		"US": "Washington", "CN": "Beijing", "JP": "Tokyo", "DE": "Berlin", "GB": "London",
		"FR": "Paris", "IT": "Rome", "ES": "Madrid", "CA": "Ottawa", "AU": "Canberra",
		"BR": "Brasília", "IN": "New Delhi", "RU": "Moscow", "KR": "Seoul", "MX": "Mexico City",
		"NL": "Amsterdam", "SE": "Stockholm", "NO": "Oslo", "DK": "Copenhagen", "FI": "Helsinki",
		"CH": "Bern", "AT": "Vienna", "BE": "Brussels", "IE": "Dublin", "PT": "Lisbon",
		"GR": "Athens", "PL": "Warsaw", "CZ": "Prague", "HU": "Budapest", "SK": "Bratislava",
		"HK": "Hong Kong", "SG": "Singapore", "TW": "Taipei", "TH": "Bangkok", "MY": "Kuala Lumpur",
	}
	if capital, ok := capitals[countryCode]; ok {
		return capital
	}
	return ""
}

// getCountryTLD 返回给定国家代码的顶级域名
func getCountryTLD(countryCode string) string {
	tlds := map[string]string{
		"US": ".us", "CN": ".cn", "JP": ".jp", "DE": ".de", "GB": ".uk",
		"FR": ".fr", "IT": ".it", "ES": ".es", "CA": ".ca", "AU": ".au",
		"BR": ".br", "IN": ".in", "RU": ".ru", "KR": ".kr", "MX": ".mx",
		"NL": ".nl", "SE": ".se", "NO": ".no", "DK": ".dk", "FI": ".fi",
		"CH": ".ch", "AT": ".at", "BE": ".be", "IE": ".ie", "PT": ".pt",
		"GR": ".gr", "PL": ".pl", "CZ": ".cz", "HU": ".hu", "SK": ".sk",
		"HK": ".hk", "SG": ".sg", "TW": ".tw", "TH": ".th", "MY": ".my",
	}
	if tld, ok := tlds[countryCode]; ok {
		return tld
	}
	return ""
}

// getCountryCallingCode 返回给定国家代码的电话区号
func getCountryCallingCode(countryCode string) string {
	callingCodes := map[string]string{
		"US": "+1", "CN": "+86", "JP": "+81", "DE": "+49", "GB": "+44",
		"FR": "+33", "IT": "+39", "ES": "+34", "CA": "+1", "AU": "+61",
		"BR": "+55", "IN": "+91", "RU": "+7", "KR": "+82", "MX": "+52",
		"NL": "+31", "SE": "+46", "NO": "+47", "DK": "+45", "FI": "+358",
		"CH": "+41", "AT": "+43", "BE": "+32", "IE": "+353", "PT": "+351",
		"GR": "+30", "PL": "+48", "CZ": "+420", "HU": "+36", "SK": "+421",
		"HK": "+852", "SG": "+65", "TW": "+886", "TH": "+66", "MY": "+60",
	}
	if code, ok := callingCodes[countryCode]; ok {
		return code
	}
	return ""
}

// getCurrency 返回给定国家代码的货币代码
func getCurrency(countryCode string) string {
	currencies := map[string]string{
		"US": "USD", "CN": "CNY", "JP": "JPY", "DE": "EUR", "GB": "GBP",
		"FR": "EUR", "IT": "EUR", "ES": "EUR", "CA": "CAD", "AU": "AUD",
		"BR": "BRL", "IN": "INR", "RU": "RUB", "KR": "KRW", "MX": "MXN",
		"NL": "EUR", "SE": "SEK", "NO": "NOK", "DK": "DKK", "FI": "EUR",
		"CH": "CHF", "AT": "EUR", "BE": "EUR", "IE": "EUR", "PT": "EUR",
		"GR": "EUR", "PL": "PLN", "CZ": "CZK", "HU": "HUF", "SK": "EUR",
		"HK": "HKD", "SG": "SGD", "TW": "TWD", "TH": "THB", "MY": "MYR",
	}
	if currency, ok := currencies[countryCode]; ok {
		return currency
	}
	return ""
}

// getCurrencyName 返回给定国家代码的货币名称
func getCurrencyName(countryCode string) string {
	currencyNames := map[string]string{
		"US": "Dollar", "CN": "Yuan", "JP": "Yen", "DE": "Euro", "GB": "Pound",
		"FR": "Euro", "IT": "Euro", "ES": "Euro", "CA": "Dollar", "AU": "Dollar",
		"BR": "Real", "IN": "Rupee", "RU": "Ruble", "KR": "Won", "MX": "Peso",
		"NL": "Euro", "SE": "Krona", "NO": "Krone", "DK": "Krone", "FI": "Euro",
		"CH": "Franc", "AT": "Euro", "BE": "Euro", "IE": "Euro", "PT": "Euro",
		"GR": "Euro", "PL": "Zloty", "CZ": "Koruna", "HU": "Forint", "SK": "Euro",
		"HK": "Dollar", "SG": "Dollar", "TW": "Dollar", "TH": "Baht", "MY": "Ringgit",
	}
	if name, ok := currencyNames[countryCode]; ok {
		return name
	}
	return ""
}

// getLanguages 返回给定国家代码的语言
func getLanguages(countryCode string) string {
	languages := map[string]string{
		"US": "en-US,es-US,haw", "CN": "zh-CN,yue,wuu,dta,ug,za", "JP": "ja", "DE": "de", "GB": "en-GB,cy-GB,gd",
		"FR": "fr-FR,frp,br,co,ca,eu,oc", "IT": "it-IT,de-IT,fr-IT,sc,ca,co,sl", "ES": "es-ES,ca,gl,eu,oc", "CA": "en-CA,fr-CA,iu", "AU": "en-AU",
		"BR": "pt-BR,en,es,de,it,ja,ko,zh", "IN": "en-IN,hi,bn,te,mr,ta,ur,gu,kn,ml,or,pa,as,bh,sat,ks,ne,sd,kok,brx,ks", "RU": "ru,tt,xal,cau,ady,kv,ce,tyv,cv,udm,tut,mns,bua,myv,mdf,chm,ba,inh,tut,kbd,krc,av,sah,nog", "KR": "ko-KR,en", "MX": "es-MX,en",
		"NL": "nl-NL,fy-NL", "SE": "sv-SE,se,sma,fi-SE", "NO": "no,nb,nn,se,fi", "DK": "da-DK,en,fo,de-DK", "FI": "fi-FI,sv-FI,smn",
		"CH": "de-CH,fr-CH,it-CH,rm", "AT": "de-AT,hr,hu,sl", "BE": "nl-BE,fr-BE,de-BE", "IE": "en-IE,ga-IE", "PT": "pt-PT,mwl",
		"GR": "el-GR,en,fr", "PL": "pl", "CZ": "cs,sk", "HU": "hu", "SK": "sk,hu",
		"HK": "zh-HK,yue,zh,en", "SG": "cmn,en-SG,ms-SG,ta-SG,zh-SG", "TW": "zh-TW,zh,nan,hak", "TH": "th,en", "MY": "ms-MY,en,zh,ta,te,ml,pa,th",
	}
	if langs, ok := languages[countryCode]; ok {
		return langs
	}
	return ""
}

// getUTCOffset 返回给定时区的 UTC 偏移量
func getUTCOffset(timezone string) string {
	// 这是一个简化的实现。在实际应用中，
	// 您应该使用适当的时区库来获取当前偏移量
	offsets := map[string]string{
		"America/Los_Angeles": "-0700",
		"America/Denver":      "-0600",
		"America/Chicago":     "-0500",
		"America/New_York":    "-0400",
		"Europe/London":       "+0100",
		"Europe/Paris":        "+0200",
		"Europe/Berlin":       "+0200",
		"Asia/Tokyo":          "+0900",
		"Asia/Shanghai":       "+0800",
		"Asia/Hong_Kong":      "+0800",
		"Asia/Singapore":      "+0800",
		"Australia/Sydney":    "+1100",
	}
	if offset, ok := offsets[timezone]; ok {
		return offset
	}
	return ""
}

// getCountryArea 返回国家的面积（平方公里）
func getCountryArea(countryCode string) float64 {
	countryAreas := map[string]float64{
		"US": 9833517.0,
		"CN": 9596960.0,
		"CA": 9984670.0,
		"RU": 17098242.0,
		"BR": 8514877.0,
		"AU": 7692024.0,
		"IN": 3287263.0,
		"AR": 2780400.0,
		"KZ": 2724900.0,
		"DZ": 2381741.0,
		"SA": 2149690.0,
		"MX": 1964375.0,
		"ID": 1904569.0,
		"SD": 1861484.0,
		"LY": 1759540.0,
		"IR": 1648195.0,
		"MN": 1564110.0,
		"PE": 1285216.0,
		"TD": 1284000.0,
		"NE": 1267000.0,
		"AO": 1246700.0,
		"ML": 1240192.0,
		"ZA": 1221037.0,
		"CO": 1141748.0,
		"ET": 1104300.0,
		"BO": 1098581.0,
		"MR": 1030700.0,
		"EG": 1001449.0,
		"TZ": 947300.0,
		"NG": 923768.0,
		"VE": 912050.0,
		"PK": 881913.0,
		"CL": 756096.0,
		"ZM": 752618.0,
		"MM": 676578.0,
		"AF": 652230.0,
		"SO": 637657.0,
		"CF": 622984.0,
		"UA": 603550.0,
		"MG": 587041.0,
		"BW": 581730.0,
		"KE": 580367.0,
		"FR": 551695.0,
		"YE": 527968.0,
		"TH": 513120.0,
		"ES": 505992.0,
		"TM": 488100.0,
		"CM": 475442.0,
		"PG": 462840.0,
		"UZ": 447400.0,
		"MA": 446550.0,
		"IQ": 438317.0,
		"PY": 406752.0,
		"ZW": 390757.0,
		"NO": 385207.0,
		"JP": 377930.0,
		"DE": 357114.0,
		"FI": 338424.0,
		"VN": 331212.0,
		"MY": 330803.0,
		"PL": 312696.0,
		"OM": 309500.0,
		"IT": 301336.0,
		"PH": 300000.0,
		"EC": 283561.0,
		"BF": 274222.0,
		"NZ": 268838.0,
		"GA": 267668.0,
		"WS": 2842.0,
		"GN": 245857.0,
		"UK": 242495.0,
		"GB": 242495.0,
		"UG": 241550.0,
		"GH": 238533.0,
		"RO": 238391.0,
		"LA": 236800.0,
		"GY": 214969.0,
		"BY": 207600.0,
		"KG": 199951.0,
		"SN": 196722.0,
		"SY": 185180.0,
		"KH": 181035.0,
		"UY": 176215.0,
		"TN": 163610.0,
		"SR": 163820.0,
		"BD": 147570.0,
		"NP": 147181.0,
		"TJ": 143100.0,
		"GR": 131957.0,
		"NI": 130373.0,
		"KP": 120538.0,
		"ER": 117600.0,
		"BG": 110879.0,
		"CU": 109884.0,
		"IS": 103000.0,
		"JO": 89342.0,
		"AZ": 86600.0,
		"AT": 83871.0,
		"AE": 83600.0,
		"CZ": 78867.0,
		"RS": 77474.0,
		"PA": 75417.0,
		"IE": 70273.0,
		"GE": 69700.0,
		"LK": 65610.0,
		"LT": 65300.0,
		"LV": 64559.0,
		"TG": 56785.0,
		"HR": 56594.0,
		"BA": 51197.0,
		"CR": 51100.0,
		"SK": 49035.0,
		"EE": 45228.0,
		"DK": 43094.0,
		"NL": 41850.0,
		"CH": 41285.0,
		"BH": 760.0,
		"GW": 36125.0,
		"MD": 33846.0,
		"BE": 30528.0,
		"AM": 29743.0,
		"AL": 28748.0,
		"SL": 71740.0,
		"EQ": 28051.0,
		"BJ": 112622.0,
		"HT": 27750.0,
		"RW": 26338.0,
		"MK": 25713.0,
		"DJ": 23200.0,
		"BZ": 22966.0,
		"IL": 20770.0,
		"SV": 21041.0,
		"SI": 20273.0,
		"FJ": 18272.0,
		"KW": 17818.0,
		"SZ": 17364.0,
		"ME": 13812.0,
		"VU": 12189.0,
		"QA": 11586.0,
		"GM": 11295.0,
		"JM": 10991.0,
		"LB": 10452.0,
		"CY": 9251.0,
		"PR": 8870.0,
		"PS": 6220.0,
		"BN": 5765.0,
		"TT": 5130.0,
		"CV": 4033.0,
		"LU": 2586.0,
		"KM": 2235.0,
		"MU": 2040.0,
		"FO": 1393.0,
		"ST": 964.0,
		"KI": 811.0,
		"DM": 751.0,
		"TO": 747.0,
		"MH": 181.0,
		"KN": 261.0,
		"PW": 459.0,
		"LC": 539.0,
		"AD": 468.0,
		"AG": 442.0,
		"BB": 430.0,
		"TV": 26.0,
		"SC": 455.0,
		"GD": 344.0,
		"MT": 316.0,
		"MV": 298.0,
		"VC": 389.0,
		"LI": 160.0,
		"SM": 61.0,
		"NR": 21.0,
		"MC": 2.02,
		"VA": 0.17,
		"HK": 1092.0,
		"SG": 719.0,
		"MO": 115.3,
	}
	if area, ok := countryAreas[countryCode]; ok {
		return area
	}
	return 0
}

// isEUCountry 如果国家是欧盟成员则返回 true
func isEUCountry(countryCode string) bool {
	euCountries := map[string]bool{
		"AT": true, // Austria
		"BE": true, // Belgium
		"BG": true, // Bulgaria
		"HR": true, // Croatia
		"CY": true, // Cyprus
		"CZ": true, // Czech Republic
		"DK": true, // Denmark
		"EE": true, // Estonia
		"FI": true, // Finland
		"FR": true, // France
		"DE": true, // Germany
		"GR": true, // Greece
		"HU": true, // Hungary
		"IE": true, // Ireland
		"IT": true, // Italy
		"LV": true, // Latvia
		"LT": true, // Lithuania
		"LU": true, // Luxembourg
		"MT": true, // Malta
		"NL": true, // Netherlands
		"PL": true, // Poland
		"PT": true, // Portugal
		"RO": true, // Romania
		"SK": true, // Slovakia
		"SI": true, // Slovenia
		"ES": true, // Spain
		"SE": true, // Sweden
	}
	return euCountries[countryCode]
}

// isCityState 如果国家是城市国家则返回 true
func isCityState(countryCode string) bool {
	cityStates := map[string]bool{
		"HK": true, // Hong Kong
		"SG": true, // Singapore
		"MC": true, // Monaco
		"VA": true, // Vatican City
		"SM": true, // San Marino
		"LI": true, // Liechtenstein
		"AD": true, // Andorra
		"MT": true, // Malta
		"MO": true, // Macau
	}
	return cityStates[countryCode]
}

// getCountryPopulation 返回国家的人口
func getCountryPopulation(countryCode string) int64 {
	countryPopulations := map[string]int64{
		"CN": 1439323776,
		"IN": 1380004385,
		"US": 331002651,
		"ID": 273523615,
		"PK": 220892340,
		"BR": 212559417,
		"NG": 206139589,
		"BD": 164689383,
		"RU": 145934462,
		"MX": 128932753,
		"JP": 126476461,
		"PH": 109581078,
		"ET": 114963588,
		"VN": 97338579,
		"EG": 102334404,
		"DE": 83783942,
		"TR": 84339067,
		"IR": 83992949,
		"TH": 69799978,
		"GB": 67886011,
		"UK": 67886011,
		"FR": 65273511,
		"IT": 60461826,
		"TZ": 59734218,
		"ZA": 59308690,
		"MM": 54409800,
		"KR": 51269185,
		"CO": 50882891,
		"KE": 53771296,
		"UG": 45741007,
		"ES": 46754778,
		"AR": 45195774,
		"DZ": 43851044,
		"SD": 43849260,
		"UA": 43733762,
		"IQ": 40222493,
		"AF": 38928346,
		"PL": 37846611,
		"CA": 37742154,
		"MA": 36910560,
		"SA": 34813871,
		"UZ": 33469203,
		"PE": 32971854,
		"MY": 32365999,
		"AO": 32866272,
		"MZ": 31255435,
		"GH": 31072940,
		"YE": 29825964,
		"NP": 29136808,
		"VE": 28435940,
		"MG": 27691018,
		"CM": 26545863,
		"CI": 26378274,
		"AU": 25499884,
		"NE": 24206644,
		"LK": 21413249,
		"BF": 20903273,
		"ML": 20250833,
		"RO": 19237691,
		"MW": 19129952,
		"CL": 19116201,
		"KZ": 18776707,
		"ZM": 18383955,
		"GT": 17915568,
		"EC": 17643054,
		"SY": 17500658,
		"NL": 17134872,
		"SN": 16743927,
		"KH": 16718965,
		"TD": 16425864,
		"SO": 15893222,
		"ZW": 14862924,
		"GN": 13132795,
		"RW": 12952218,
		"BJ": 12123200,
		"TN": 11818619,
		"BI": 11890784,
		"BO": 11673021,
		"BE": 11589623,
		"HT": 11402528,
		"CU": 11326616,
		"SS": 11193725,
		"DO": 10847910,
		"CZ": 10708981,
		"GR": 10423054,
		"JO": 10203134,
		"PT": 10196709,
		"AZ": 10139177,
		"SE": 10099265,
		"HU": 9660351,
		"BY": 9449323,
		"TJ": 9537645,
		"AE": 9890402,
		"HN": 9904607,
		"AT": 9006398,
		"PG": 8947024,
		"IL": 8655535,
		"CH": 8654622,
		"TG": 8278724,
		"SL": 7976983,
		"HK": 7451000,
		"LA": 7275560,
		"PY": 7132538,
		"BG": 6948445,
		"LY": 6871292,
		"LB": 6825445,
		"NI": 6624554,
		"KG": 6524195,
		"ER": 3546421,
		"UY": 3473730,
		"MN": 3278290,
		"BA": 3280819,
		"AM": 2963243,
		"JM": 2961167,
		"QA": 2881053,
		"AL": 2877797,
		"PR": 2860853,
		"LT": 2722289,
		"MR": 4649658,
		"PA": 4314767,
		"CR": 5094118,
		"SK": 5459642,
		"NO": 5421241,
		"IE": 4937786,
		"HR": 4105267,
		"NZ": 4822233,
		"GE": 3989167,
		"LV": 1886198,
		"EE": 1326535,
		"MU": 1271768,
		"CY": 1207359,
		"FI": 5540720,
		"DK": 5792202,
		"SG": 5850342,
		"KW": 4270571,
		"OM": 5106626,
		"MD": 4033963,
		"BH": 1701575,
		"TT": 1399488,
		"EQ": 1402985,
		"SZ": 1160164,
		"DJ": 988000,
		"FJ": 896445,
		"RE": 895312,
		"GP": 400124,
		"BT": 771608,
		"GY": 786552,
		"SB": 686884,
		"MO": 649335,
		"LU": 625978,
		"ME": 628066,
		"WS": 198414,
		"CV": 555987,
		"BN": 437479,
		"MT": 441543,
		"BZ": 397628,
		"MV": 540544,
		"IS": 341243,
		"VU": 307145,
		"BB": 287375,
		"ST": 219159,
		"WF": 11239,
		"LC": 183627,
		"KI": 119449,
		"FM": 115023,
		"GD": 112523,
		"VC": 110940,
		"TO": 105695,
		"SC": 98347,
		"AG": 97929,
		"AD": 77265,
		"DM": 71986,
		"MH": 59190,
		"KN": 53199,
		"FO": 48863,
		"LI": 38128,
		"MC": 39242,
		"SM": 33931,
		"PW": 18094,
		"CK": 17564,
		"NR": 10824,
		"TV": 11792,
		"VA": 801,
	}
	if population, ok := countryPopulations[countryCode]; ok {
		return population
	}
	return 0
}

// getChinesePostalCode 返回中国地区的代表性邮政编码

// inferChineseRegionFromCoordinates 尝试从坐标推断中国省份
func getChinesePostalCode(regionCode, regionName string) string {
	// 中国省份/地区代码到代表性邮政编码的映射
	postalMap := map[string]string{
		"BJ": "100000", // Beijing
		"TJ": "300000", // Tianjin
		"HE": "050000", // Hebei
		"SX": "030000", // Shanxi
		"NM": "010000", // Inner Mongolia
		"LN": "110000", // Liaoning
		"JL": "130000", // Jilin
		"HL": "150000", // Heilongjiang
		"SH": "200000", // Shanghai
		"JS": "210000", // Jiangsu
		"ZJ": "310000", // Zhejiang
		"AH": "230000", // Anhui
		"FJ": "350000", // Fujian
		"JX": "330000", // Jiangxi
		"SD": "250000", // Shandong
		"HA": "450000", // Henan
		"HB": "430000", // Hubei
		"HN": "410000", // Hunan
		"GD": "510000", // Guangdong
		"GX": "530000", // Guangxi
		"HI": "570000", // Hainan
		"CQ": "400000", // Chongqing
		"SC": "610000", // Sichuan
		"GZ": "550000", // Guizhou
		"YN": "650000", // Yunnan
		"XZ": "850000", // Tibet
		"SN": "710000", // Shaanxi
		"GS": "730000", // Gansu
		"QH": "810000", // Qinghai
		"NX": "750000", // Ningxia
		"XJ": "830000", // Xinjiang
	}

	if regionCode != "" {
		if postal, exists := postalMap[regionCode]; exists {
			return postal
		}
	}

	// 基于地区名称的回退
	regionNameMap := map[string]string{
		"北京":  "100000",
		"天津":  "300000",
		"河北":  "050000",
		"山西":  "030000",
		"内蒙古": "010000",
		"辽宁":  "110000",
		"吉林":  "130000",
		"黑龙江": "150000",
		"上海":  "200000",
		"江苏":  "210000",
		"浙江":  "310000",
		"安徽":  "230000",
		"福建":  "350000",
		"江西":  "330000",
		"山东":  "250000",
		"河南":  "450000",
		"湖北":  "430000",
		"湖南":  "410000",
		"广东":  "510000",
		"广西":  "530000",
		"海南":  "570000",
		"重庆":  "400000",
		"四川":  "610000",
		"贵州":  "550000",
		"云南":  "650000",
		"西藏":  "850000",
		"陕西":  "710000",
		"甘肃":  "730000",
		"青海":  "810000",
		"宁夏":  "750000",
		"新疆":  "830000",
	}

	if regionName != "" {
		if postal, exists := regionNameMap[regionName]; exists {
			return postal
		}
	}

	return ""
}

// StaticMapHandler 处理静态地图请求
func StaticMapHandler(w http.ResponseWriter, r *http.Request) {
	// 检查 Geoapify API 密钥是否配置
	if config.App.GeoapifyAPIKey == "" {
		log.Println("Geoapify API key not configured")
		http.Error(w, "Static map service not available", http.StatusServiceUnavailable)
		return
	}

	// 获取查询参数
	queryParams := r.URL.Query()

	// 构建 Geoapify Static Map API URL
	geoapifyURL := "https://maps.geoapify.com/v1/staticmap"
	params := url.Values{}

	// 添加 API 密钥
	params.Set("apiKey", config.App.GeoapifyAPIKey)

	// 转发所有查询参数（除了可能的 apiKey）
	for key, values := range queryParams {
		if key != "apiKey" { // 防止客户端覆盖我们的 API 密钥
			for _, value := range values {
				params.Add(key, value)
			}
		}
	}

	// 构建完整的请求 URL
	fullURL := geoapifyURL + "?" + params.Encode()

	// 创建缓存键
	cacheKey := "staticmap:" + params.Encode()

	// 检查缓存
	if cachedData, found := ipCache.Get(cacheKey); found {
		log.Printf("Serving static map from cache")
		w.Header().Set("X-Cache", "HIT")
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(cachedData.([]byte))
		return
	}

	// 向 Geoapify 发起请求
	resp, err := http.Get(fullURL)
	if err != nil {
		log.Printf("Error fetching static map: %v", err)
		http.Error(w, "Failed to fetch static map", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		log.Printf("Geoapify API returned status: %d", resp.StatusCode)
		http.Error(w, "Static map service error", resp.StatusCode)
		return
	}

	// 读取响应数据
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading static map response: %v", err)
		http.Error(w, "Failed to read static map", http.StatusInternalServerError)
		return
	}

	// 缓存图片数据（缓存1小时）
	ipCache.Set(cacheKey, imageData, 1*time.Hour)

	// 设置响应头
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Cache", "MISS")

	// 返回图片数据
	w.Write(imageData)

	log.Printf("Static map served successfully")
}
