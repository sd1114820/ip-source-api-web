package main

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
)

// 模拟缓存
var testCache = cache.New(5*time.Minute, 10*time.Minute)

// 修复后的StaticMapHandler
func FixedStaticMapHandler(w http.ResponseWriter, r *http.Request) {
	// 使用测试API密钥
	apiKey := "your_geoapify_api_key_here" // 请替换为真实的API密钥
	if apiKey == "your_geoapify_api_key_here" {
		log.Println("请在代码中设置真实的Geoapify API密钥")
		http.Error(w, "Static map service not available - API key not configured", http.StatusServiceUnavailable)
		return
	}

	// 获取原始查询字符串
	rawQuery := r.URL.RawQuery
	
	// 构建 Geoapify Static Map API URL
	geoapifyURL := "https://maps.geoapify.com/v1/staticmap"
	
	// 构建完整的请求 URL，保持原始编码
	var fullURL string
	if rawQuery != "" {
		// 检查是否已经包含apiKey参数
		if strings.Contains(rawQuery, "apiKey=") {
			// 替换现有的apiKey
			fullURL = geoapifyURL + "?" + rawQuery
		} else {
			// 添加我们的apiKey
			fullURL = geoapifyURL + "?" + rawQuery + "&apiKey=" + url.QueryEscape(apiKey)
		}
	} else {
		// 只有apiKey
		fullURL = geoapifyURL + "?apiKey=" + url.QueryEscape(apiKey)
	}
	
	log.Printf("原始查询: %s", rawQuery)
	log.Printf("转发到Geoapify: %s", fullURL)

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
		// 读取错误响应内容
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Error response: %s", string(body))
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

	// 设置响应头
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// 返回图片数据
	w.Write(imageData)

	log.Printf("Static map served successfully, size: %d bytes", len(imageData))
}

func main() {
	http.HandleFunc("/map", FixedStaticMapHandler)
	
	log.Println("测试服务器启动在 :8181")
	log.Println("请在代码中设置真实的Geoapify API密钥")
	log.Println("测试URL: http://localhost:8181/map?style=osm-carto&width=600&height=400&center=lonlat:116.4074,39.9042&zoom=12&marker=lonlat:116.4074,39.9042;type:material;size:medium;icon:marker&format=jpeg&lang=zh")
	
	if err := http.ListenAndServe(":8181", nil); err != nil {
		log.Fatal(err)
	}
}