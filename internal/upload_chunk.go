package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (sm *ServerManager) getUploadTargetAndPartPath(cfg Config, itemID, subPath, relPath, fileID string) (string, string, error) {
	targetDir := cfg.TempDir
	if itemID != "" {
		resolved, it, err := sm.resolveRootOrSub(cfg, itemID, subPath)
		if err == nil && it != nil && it.IsDir {
			targetDir = resolved
		}
	}

	cleanedRel := filepath.Base(relPath)
	if strings.TrimSpace(relPath) != "" {
		c := filepath.Clean("/" + strings.TrimSpace(relPath))
		c = strings.TrimPrefix(c, "/")
		if c != "" && c != "." {
			cleanedRel = c
		}
	}

	destPath := filepath.Join(targetDir, cleanedRel)
	destAbs, err := filepath.Abs(destPath)
	baseAbs, _ := filepath.Abs(targetDir)
	if err != nil || !strings.HasPrefix(destAbs, baseAbs) {
		return "", "", fmt.Errorf("invalid destination path")
	}

	_ = os.MkdirAll(filepath.Dir(destAbs), 0755)

	hashInput := fmt.Sprintf("%s|%s", destAbs, fileID)
	sum := sha256.Sum256([]byte(hashInput))
	partName := ".tinydrop_part_" + hex.EncodeToString(sum[:8])
	partPath := filepath.Join(filepath.Dir(destAbs), partName)

	return destAbs, partPath, nil
}

// handleUploadInit checks existing .part progress so interrupted large uploads can resume seamlessly
func (sm *ServerManager) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	cfg := sm.CM.GetSnapshot()
	if !sm.canManage(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Read-only mode: client upload permission is disabled"})
		return
	}

	var req struct {
		ItemID  string `json:"item_id"`
		SubPath string `json:"sub_path"`
		RelPath string `json:"rel_path"`
		FileID  string `json:"file_id"`
		Size    int64  `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
		return
	}

	_, partPath, err := sm.getUploadTargetAndPartPath(cfg, req.ItemID, req.SubPath, req.RelPath, req.FileID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var offset int64 = 0
	if info, err := os.Stat(partPath); err == nil && !info.IsDir() {
		if info.Size() <= req.Size {
			offset = info.Size()
		} else {
			_ = os.Remove(partPath)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":     true,
		"offset": offset,
	})
}

// handleUploadChunk streams a binary chunk at exact offset and finalizes when complete
func (sm *ServerManager) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	cfg := sm.CM.GetSnapshot()
	if !sm.canManage(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Upload permission denied"})
		return
	}

	q := r.URL.Query()
	itemID := q.Get("item_id")
	subPath := q.Get("sub_path")
	relPath := q.Get("rel_path")
	fileID := q.Get("file_id")
	offset, _ := strconv.ParseInt(q.Get("offset"), 10, 64)
	totalSize, _ := strconv.ParseInt(q.Get("total"), 10, 64)

	destAbs, partPath, err := sm.getUploadTargetAndPartPath(cfg, itemID, subPath, relPath, fileID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to open temporary chunk file: " + err.Error()})
		return
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Seek failed: " + err.Error()})
		return
	}

	written, err := io.Copy(f, r.Body)
	_ = f.Close()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Chunk stream interrupted: " + err.Error()})
		return
	}

	newOffset := offset + written
	completed := false
	if totalSize >= 0 && newOffset >= totalSize {
		// If destination already exists, pick a unique name or overwrite
		finalDest := destAbs
		if _, err := os.Stat(finalDest); err == nil {
			finalDest = UniqueDestPath(filepath.Dir(destAbs), filepath.Base(destAbs))
		}
		if err := os.Rename(partPath, finalDest); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to finalize uploaded file: " + err.Error()})
			return
		}
		completed = true
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"offset":    newOffset,
		"completed": completed,
	})
}

// handleUploadCancel removes the temporary .part file when the user cancels/removes an upload task
func (sm *ServerManager) handleUploadCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	cfg := sm.CM.GetSnapshot()
	if !sm.canManage(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Permission denied"})
		return
	}
	var req struct {
		ItemID  string `json:"item_id"`
		SubPath string `json:"sub_path"`
		RelPath string `json:"rel_path"`
		FileID  string `json:"file_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	_, partPath, err := sm.getUploadTargetAndPartPath(cfg, req.ItemID, req.SubPath, req.RelPath, req.FileID)
	if err == nil && partPath != "" {
		_ = os.Remove(partPath)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
