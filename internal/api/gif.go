package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/ppxb/miyabi/internal/domain"
)

const (
	gifDirName     = "gif"
	gifMaxBytes    = 32 << 20 // 32 MiB
	gifHashPrefix  = 16       // hex chars used in filenames
	gifFormFileKey = "gif"
	gifFormNameKey = "name"
)

type gifSaveResponse struct {
	Filename  string `json:"filename"`
	Relative  string `json:"relative_path"`
	Size      int64  `json:"size"`
	Duplicate bool   `json:"duplicate"`
	Hash      string `json:"hash"`
}

func saveGifHandler(dataDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, err := saveGifUpload(dataDir, c.Request)
		respond(c, result, err)
	}
}

func saveGifUpload(dataDir string, req *http.Request) (*gifSaveResponse, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, domain.E(domain.KindUnexpected, "数据目录未配置", nil)
	}

	if err := req.ParseMultipartForm(gifMaxBytes); err != nil {
		// Allow raw body as image/gif for simple clients.
		if req.Body == nil {
			return nil, badRequest(fmt.Errorf("无法读取上传内容: %w", err))
		}
		limited := io.LimitReader(req.Body, gifMaxBytes+1)
		raw, readErr := io.ReadAll(limited)
		if readErr != nil {
			return nil, badRequest(fmt.Errorf("无法读取上传内容: %w", readErr))
		}
		if int64(len(raw)) > gifMaxBytes {
			return nil, badRequest(fmt.Errorf("GIF 超过大小限制 (%d MiB)", gifMaxBytes>>20))
		}
		name := strings.TrimSpace(req.URL.Query().Get("name"))
		return writeGifFile(dataDir, name, raw)
	}

	file, header, err := req.FormFile(gifFormFileKey)
	if err != nil {
		return nil, badRequest(fmt.Errorf("缺少 gif 文件字段: %w", err))
	}
	defer file.Close()

	limited := io.LimitReader(file, gifMaxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, badRequest(fmt.Errorf("读取 GIF 失败: %w", err))
	}
	if int64(len(raw)) > gifMaxBytes {
		return nil, badRequest(fmt.Errorf("GIF 超过大小限制 (%d MiB)", gifMaxBytes>>20))
	}
	if len(raw) < 6 || (string(raw[:6]) != "GIF87a" && string(raw[:6]) != "GIF89a") {
		return nil, badRequest(fmt.Errorf("文件不是有效的 GIF"))
	}

	name := strings.TrimSpace(req.FormValue(gifFormNameKey))
	if name == "" && header != nil {
		name = header.Filename
	}
	return writeGifFile(dataDir, name, raw)
}

func writeGifFile(dataDir, requestedName string, raw []byte) (*gifSaveResponse, error) {
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	prefix := hash[:gifHashPrefix]

	dir := filepath.Join(dataDir, gifDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, domain.E(domain.KindUnexpected, "创建 gif 目录失败", err)
	}

	if existing, ok, err := findGifByHashPrefix(dir, prefix); err != nil {
		return nil, domain.E(domain.KindUnexpected, "检查已有 GIF 失败", err)
	} else if ok {
		info, statErr := os.Stat(existing)
		size := int64(len(raw))
		if statErr == nil {
			size = info.Size()
		}
		base := filepath.Base(existing)
		return &gifSaveResponse{
			Filename:  base,
			Relative:  filepath.ToSlash(filepath.Join(gifDirName, base)),
			Size:      size,
			Duplicate: true,
			Hash:      hash,
		}, nil
	}

	base := buildGifFilename(prefix, requestedName)
	path := filepath.Join(dir, base)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return nil, domain.E(domain.KindUnexpected, "写入 GIF 失败", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, domain.E(domain.KindUnexpected, "保存 GIF 失败", err)
	}

	return &gifSaveResponse{
		Filename:  base,
		Relative:  filepath.ToSlash(filepath.Join(gifDirName, base)),
		Size:      int64(len(raw)),
		Duplicate: false,
		Hash:      hash,
	}, nil
}

func findGifByHashPrefix(dir, prefix string) (string, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	needle := prefix + "_"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, needle) && strings.HasSuffix(strings.ToLower(name), ".gif") {
			return filepath.Join(dir, name), true, nil
		}
		// Legacy / hash-only names: <prefix>.gif
		if name == prefix+".gif" {
			return filepath.Join(dir, name), true, nil
		}
	}
	return "", false, nil
}

var gifUnsafe = regexp.MustCompile(`[^\p{L}\p{N}\-_.=+]+`)

func buildGifFilename(prefix, requested string) string {
	name := strings.TrimSpace(requested)
	name = filepath.Base(name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = gifUnsafe.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._")
	if name == "" || !hasVisible(name) {
		name = "clip"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return prefix + "_" + name + ".gif"
}

func hasVisible(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}
