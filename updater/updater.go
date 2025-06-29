package updater

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ip-api/config"
	"ip-api/geoip"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// dbSources 定义了数据库的来源信息
var dbSources = map[string]struct {
	EditionID string // MaxMind的数据库版本ID
	URL       string // 针对非MaxMind源的直接URL
}{
	"GeoLite2-City.mmdb": {EditionID: "GeoLite2-City"},
	"GeoLite2-ASN.mmdb":  {EditionID: "GeoLite2-ASN"},
	"GeoCN.mmdb":         {URL: "https://github.com/ljxi/GeoCN/releases/download/Latest/GeoCN.mmdb"},
}

// DownloadAll 下载所有数据库文件，等待它们完成。
// 这用于初始设置。
func DownloadAll() {
	log.Println("Performing initial database download...")
	var wg sync.WaitGroup
	for fileName, source := range dbSources {
		wg.Add(1)
		go func(fileName string, source struct {
			EditionID string
			URL       string
		}) {
			defer wg.Done()
			var err error
			if source.EditionID != "" {
				// 从 MaxMind 下载
				err = downloadFromMaxMind(source.EditionID, fileName)
			} else {
				// 从指定 URL 下载
				err = downloadFromURL(source.URL, fileName)
			}
			if err != nil {
				log.Printf("Failed to download %s: %v", fileName, err)
			} else {
				log.Printf("Successfully downloaded %s", fileName)
			}
		}(fileName, source)
	}
	wg.Wait()
	log.Println("Initial database download finished.")
}

// Start 运行定时器定期更新数据库。
// 它遵循上下文以实现优雅关闭。
func Start() {
	ticker := time.NewTicker(time.Duration(config.App.UpdateInterval) * time.Hour)
	defer ticker.Stop()

	checkForUpdates := func() {
		update()
	}

	for {
		select {
		case <-ticker.C:
			log.Println("Starting scheduled database update.")
			checkForUpdates()
		}
	}
}

// update 尝试下载所有数据库文件。
func update() {
	log.Println("Checking for database updates...")
	for fileName, source := range dbSources {
		log.Printf("Checking %s...", fileName)
		var err error
		if source.EditionID != "" {
			err = downloadFromMaxMind(source.EditionID, fileName)
		} else {
			err = downloadFromURL(source.URL, fileName)
		}

		if err != nil {
			if err == errNotModified {
				log.Printf("%s is up to date.", fileName)
			} else {
				log.Printf("Failed to update %s: %v", fileName, err)
			}
		} else {
			log.Printf("Successfully updated %s", fileName)
			// Reload databases after update
			cityDBPath := filepath.Join(config.App.DataDir, "GeoLite2-City.mmdb")
			asnDBPath := filepath.Join(config.App.DataDir, "GeoLite2-ASN.mmdb")
			cnDBPath := filepath.Join(config.App.DataDir, "GeoCN.mmdb")
			if err = geoip.ReloadDBs(cityDBPath, asnDBPath, cnDBPath); err != nil {
				log.Printf("Failed to reload databases: %v", err)
			}
		}
	}
}

var errNotModified = fmt.Errorf("not modified")

// downloadFromURL 从给定的URL下载文件
func downloadFromURL(url, fileName string) error {
	filePath := filepath.Join(config.App.DataDir, fileName)
	return downloadAndExtract(url, filePath, fileName)
}

// downloadFromMaxMind 使用许可证密钥认证从 MaxMind 下载文件
func downloadFromMaxMind(editionID, fileName string) error {
	if config.App.MaxMindLicenseKey == "" {
		return fmt.Errorf("MaxMind license key is not set")
	}
	url := fmt.Sprintf("https://download.maxmind.com/app/geoip_download?edition_id=%s&license_key=%s&suffix=tar.gz", editionID, config.App.MaxMindLicenseKey)

	filePath := filepath.Join(config.App.DataDir, fileName)
	return downloadAndExtract(url, filePath, fileName)
}

// downloadAndExtract 下载并提取 tar.gz 文件
func downloadAndExtract(url, filePath, etagFileName string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	etagFilePath := filepath.Join(config.App.DataDir, etagFileName+".etag")
	if etag, err := readETag(etagFilePath); err == nil {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return errNotModified
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	// 下载文件
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		// 检查是否为HTML错误页面
		if strings.Contains(strings.ToLower(contentType), "text/html") {
			return fmt.Errorf("received HTML content instead of binary file, Content-Type: %s", contentType)
		}
		// 对于tar.gz文件，检查是否为合适的类型
		if strings.Contains(url, "suffix=tar.gz") || strings.HasSuffix(url, ".tar.gz") {
			if !strings.Contains(strings.ToLower(contentType), "gzip") &&
				!strings.Contains(strings.ToLower(contentType), "tar") &&
				!strings.Contains(strings.ToLower(contentType), "octet-stream") {
				log.Printf("Warning: unexpected Content-Type for tar.gz file: %s", contentType)
			}
		}
	}

	// 将响应体写入临时文件
	tempFilePath := filePath + ".tmp_download"
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return err
	}
	defer os.Remove(tempFilePath) // 确保临时文件被清理

	// 将整个响应体读入内存以确保完整性
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// 检查最小文件大小（避免下载到错误页面）
	const minFileSize = 2 * 1024 * 1024 // 2MB 最小大小
	if len(body) < minFileSize {
		tempFile.Close()
		return fmt.Errorf("downloaded file too small (%d bytes), likely an error page", len(body))
	}

	// 轻度验证文件格式（检查前几个字节）
	if err := validateFileFormat(body, url); err != nil {
		tempFile.Close()
		return fmt.Errorf("file format validation failed: %w", err)
	}

	// 将完整的响应体写入临时文件
	if _, err := tempFile.Write(body); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write body to temp file: %w", err)
	}
	tempFile.Close() // 关闭文件以便提取器可以使用

	// 创建最终文件的临时路径
	finalTempPath := filePath + ".tmp_final"
	defer os.Remove(finalTempPath) // 确保最终临时文件被清理

	// 从 tar.gz 中提取 .mmdb 文件
	// 检查URL中是否包含suffix=tar.gz参数或以.tar.gz结尾
	if strings.Contains(url, "suffix=tar.gz") || strings.HasSuffix(url, ".tar.gz") {
		if err := extractMmdbFromTarGz(tempFilePath, finalTempPath); err != nil {
			return fmt.Errorf("failed to extract .mmdb from tar.gz: %w", err)
		}
	} else {
		// 如果不是tar.gz，复制到最终临时路径
		if err := copyFile(tempFilePath, finalTempPath); err != nil {
			return fmt.Errorf("failed to copy file to final temp path: %w", err)
		}
	}

	// 验证提取的文件
	if err := validateMmdbFile(finalTempPath); err != nil {
		return fmt.Errorf("MMDB file validation failed: %w", err)
	}

	// 原子性地替换旧文件
	if err := os.Rename(finalTempPath, filePath); err != nil {
		return fmt.Errorf("failed to replace final file: %w", err)
	}

	// 保存新的ETag
	newETag := resp.Header.Get("ETag")
	if newETag != "" {
		if err := writeETag(etagFilePath, newETag); err != nil {
			log.Printf("Warning: failed to write ETag for %s: %v", etagFileName, err)
		}
	}

	return nil
}

// extractMmdbFromTarGz 从 .tar.gz 压缩包中提取 .mmdb 文件
func extractMmdbFromTarGz(tarGzPath, destPath string) error {
	file, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // 压缩包结束
		}
		if err != nil {
			return err
		}

		if header.Typeflag == tar.TypeReg && strings.HasSuffix(header.Name, ".mmdb") {
			tempDestPath := destPath + ".tmp"
			outFile, err := os.Create(tempDestPath)
			if err != nil {
				return err
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				os.Remove(tempDestPath) // 出错时清理
				return err
			}
			outFile.Close()

			// 找到并提取了文件，现在重命名它，我们就完成了。
			if err := os.Rename(tempDestPath, destPath); err != nil {
				return err
			}
			return nil // 成功
		}
	}

	return fmt.Errorf(".mmdb file not found in archive")
}

func readETag(etagFile string) (string, error) {
	data, err := os.ReadFile(etagFile)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeETag 将 ETag 写入文件
func writeETag(etagFile, etag string) error {
	return os.WriteFile(etagFile, []byte(etag), 0644)
}

// validateFileFormat 验证文件具有正确的格式
func validateFileFormat(data []byte, url string) error {
	if len(data) < 16 {
		return fmt.Errorf("file too small for format validation")
	}

	// 检查是否为HTML内容（常见的错误页面）
	if strings.Contains(strings.ToLower(string(data[:min(512, len(data))])), "<html") ||
		strings.Contains(strings.ToLower(string(data[:min(512, len(data))])), "<!doctype") {
		return fmt.Errorf("detected HTML content, likely an error page")
	}

	// 如果是tar.gz文件，检查gzip魔数
	if strings.Contains(url, "suffix=tar.gz") || strings.HasSuffix(url, ".tar.gz") {
		if len(data) >= 2 && (data[0] != 0x1f || data[1] != 0x8b) {
			return fmt.Errorf("invalid gzip magic number")
		}
	} else {
		// 对于直接的MMDB文件，检查MMDB魔数
		// MMDB文件通常以特定的字节序列开始
		if len(data) >= 4 {
			// 检查是否包含MMDB的特征字节
			// 这是一个简单的启发式检查
			hasValidStart := false
			// MMDB文件通常在开头包含版本信息
			for i := 0; i < min(64, len(data)-4); i++ {
				if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 {
					hasValidStart = true
					break
				}
			}
			if !hasValidStart {
				log.Printf("Warning: MMDB file format validation inconclusive")
			}
		}
	}

	return nil
}

// validateMmdbFile 验证 MMDB 文件可以被打开
func validateMmdbFile(filePath string) error {
	// 尝试使用geoip2库打开文件进行验证
	db, err := geoip.OpenTestDB(filePath)
	if err != nil {
		return fmt.Errorf("failed to open MMDB file for validation: %w", err)
	}
	db.Close()
	return nil
}

// copyFile 将文件从 src 复制到 dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// min 返回两个整数中的最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
