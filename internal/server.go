package internal

import (
	"archive/zip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webEmbed embed.FS

type ServerManager struct {
	CM          *ConfigManager
	AppVersion  string
	mu          sync.Mutex
	httpServer  *http.Server
	runningPort int
	lastError   string
	stopChan    chan struct{}
}

func NewServerManager(cm *ConfigManager) *ServerManager {
	return &ServerManager{
		CM:         cm,
		AppVersion: "v1.0.3",
		stopChan:   make(chan struct{}),
	}
}

func (sm *ServerManager) StartAndKeepAlive() {
	go sm.startServerInstance()
	go sm.watchdogLoop()
}

func (sm *ServerManager) Restart() {
	sm.mu.Lock()
	srv := sm.httpServer
	sm.httpServer = nil
	sm.mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	go sm.startServerInstance()
}

func (sm *ServerManager) watchdogLoop() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopChan:
			return
		case <-ticker.C:
			cfg := sm.CM.GetSnapshot()
			if !cfg.AutoKeepAlive {
				continue
			}
			client := &http.Client{Timeout: 1500 * time.Millisecond}
			pingURL := fmt.Sprintf("http://127.0.0.1:%d/api/ping", cfg.Port)
			resp, err := client.Get(pingURL)
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil {
					_ = resp.Body.Close()
				}
				log.Printf("[Watchdog] Port %d unresponsive, auto-recovering HTTP service...", cfg.Port)
				sm.Restart()
			} else {
				_ = resp.Body.Close()
			}
		}
	}
}

func (sm *ServerManager) startServerInstance() {
	cfg := sm.CM.GetSnapshot()
	mux := http.NewServeMux()

	// Static Web Assets
	webRoot, _ := fs.Sub(webEmbed, "web")
	fileServer := http.FileServer(http.FS(webRoot))
	mux.Handle("/", fileServer)

	// API Endpoints
	mux.HandleFunc("/api/ping", sm.handlePing)
	mux.HandleFunc("/api/admin/entry", sm.handleAdminEntry)
	mux.HandleFunc("/api/auth/login", sm.handleLogin)
	mux.HandleFunc("/api/status", sm.handleStatus)
	mux.HandleFunc("/api/text", sm.handleSharedText)
	mux.HandleFunc("/api/files", sm.handleFiles)
	mux.HandleFunc("/api/files/delete", sm.handleDeleteFiles)
	mux.HandleFunc("/api/files/upload", sm.handleUpload)
	mux.HandleFunc("/api/files/upload/init", sm.handleUploadInit)
	mux.HandleFunc("/api/files/upload/chunk", sm.handleUploadChunk)
	mux.HandleFunc("/api/files/upload/cancel", sm.handleUploadCancel)
	mux.HandleFunc("/api/files/download", sm.handleDownload)
	mux.HandleFunc("/api/pc/pick", sm.handlePCPick)
	mux.HandleFunc("/api/pc/add-paths", sm.handlePCAddPaths)
	mux.HandleFunc("/api/pc/open-finder", sm.handlePCOpenFinder)
	mux.HandleFunc("/api/pc/config", sm.handlePCConfig)

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	sm.mu.Lock()
	sm.httpServer = srv
	sm.runningPort = cfg.Port
	sm.lastError = ""
	sm.mu.Unlock()

	log.Printf("[TinyDrop] HTTP Server listening on http://127.0.0.1:%d", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		sm.mu.Lock()
		sm.lastError = err.Error()
		sm.mu.Unlock()
		log.Printf("[TinyDrop] Server listen error: %v", err)
	}
}

func (sm *ServerManager) makeClientToken(cfg Config) string {
	h := hmac.New(sha256.New, []byte(cfg.AdminSecret))
	h.Write([]byte(cfg.Password + ":" + cfg.PasswordVersion))
	return hex.EncodeToString(h.Sum(nil))
}

func (sm *ServerManager) isAdmin(r *http.Request, cfg Config) bool {
	if c, err := r.Cookie("pd_admin"); err == nil && c.Value == cfg.AdminSecret {
		return true
	}
	if r.Header.Get("X-Admin-Token") == cfg.AdminSecret {
		return true
	}
	return false
}

func (sm *ServerManager) isClientAuthed(r *http.Request, cfg Config) bool {
	if sm.isAdmin(r, cfg) {
		return true
	}
	expected := sm.makeClientToken(cfg)
	if c, err := r.Cookie("pd_auth"); err == nil && c.Value == expected {
		return true
	}
	if r.Header.Get("X-Auth-Token") == expected {
		return true
	}
	if r.URL.Query().Get("token") == expected {
		return true
	}
	return false
}

func (sm *ServerManager) canManage(r *http.Request, cfg Config) bool {
	if sm.isAdmin(r, cfg) {
		return true
	}
	if !cfg.ShareEnabled {
		return false
	}
	return sm.isClientAuthed(r, cfg) && cfg.AllowClientEdit
}

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func (sm *ServerManager) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "ts": time.Now().Unix()})
}

func (sm *ServerManager) handleAdminEntry(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	secret := r.URL.Query().Get("secret")
	if secret == cfg.AdminSecret {
		http.SetCookie(w, &http.Cookie{
			Name:     "pd_admin",
			Value:    cfg.AdminSecret,
			Path:     "/",
			MaxAge:   365 * 24 * 3600,
			HttpOnly: false,
		})
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (sm *ServerManager) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	cfg := sm.CM.GetSnapshot()
	if !cfg.ShareEnabled {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "File sharing is currently disabled by the host"})
		return
	}
	if strings.TrimSpace(req.Password) != cfg.Password {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid 6-digit PIN code, please try again"})
		return
	}
	token := sm.makeClientToken(cfg)
	http.SetCookie(w, &http.Cookie{
		Name:     "pd_auth",
		Value:    token,
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		HttpOnly: false,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"token": token,
	})
}

func (sm *ServerManager) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	admin := sm.isAdmin(r, cfg)
	authed := sm.isClientAuthed(r, cfg)

	resp := map[string]interface{}{
		"app_name":          "TinyDrop",
		"version":           sm.AppVersion,
		"github_url":        "https://github.com/tim-today/TinyDrop",
		"is_admin":          admin,
		"is_authed":         authed,
		"share_enabled":     cfg.ShareEnabled,
		"allow_client_edit": cfg.AllowClientEdit,
		"can_edit":          sm.canManage(r, cfg),
		"port":              cfg.Port,
		"auto_keep_alive":   cfg.AutoKeepAlive,
	}

	if admin {
		resp["password"] = cfg.Password
		resp["share_mode"] = cfg.ShareMode
		resp["temp_dir"] = cfg.TempDir
		resp["shared_text"] = cfg.SharedText
		resp["addresses"] = GetNetworkAddresses(cfg.Port)
	} else if authed {
		resp["shared_text"] = cfg.SharedText
		resp["addresses"] = GetNetworkAddresses(cfg.Port)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (sm *ServerManager) handleSharedText(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	admin := sm.isAdmin(r, cfg)
	if !admin {
		if !cfg.ShareEnabled {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "File sharing is disabled"})
			return
		}
		if !sm.isClientAuthed(r, cfg) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "PIN authentication required"})
			return
		}
	}

	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":   true,
			"text": cfg.SharedText,
		})
		return
	}

	if r.Method == http.MethodPost {
		if !sm.canManage(r, cfg) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Read-only mode: client edit permission is disabled"})
			return
		}
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON payload"})
			return
		}
		_ = sm.CM.Update(func(c *Config) {
			c.SharedText = req.Text
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":   true,
			"text": req.Text,
		})
		return
	}

	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
}

func (sm *ServerManager) resolveRootOrSub(cfg Config, itemID, subPath string) (string, *ShareItem, error) {
	subPath = filepath.Clean("/" + subPath)
	subPath = strings.TrimPrefix(subPath, "/")
	if subPath == "." {
		subPath = ""
	}

	if itemID == "__temp__" {
		base, err := filepath.Abs(cfg.TempDir)
		if err != nil {
			return "", nil, err
		}
		target := filepath.Join(base, subPath)
		targetAbs, err := filepath.Abs(target)
		if err != nil || !strings.HasPrefix(targetAbs, base) {
			return "", nil, fmt.Errorf("invalid directory access path")
		}
		return targetAbs, &ShareItem{
			ID:    "__temp__",
			Name:  "Default Temp Share",
			Path:  base,
			IsDir: true,
			Mode:  "temp",
		}, nil
	}

	for _, it := range cfg.Items {
		if it.ID == itemID {
			base, err := filepath.Abs(it.Path)
			if err != nil {
				return "", nil, err
			}
			if !it.IsDir {
				return base, &it, nil
			}
			target := filepath.Join(base, subPath)
			targetAbs, err := filepath.Abs(target)
			if err != nil || !strings.HasPrefix(targetAbs, base) {
				return "", nil, fmt.Errorf("invalid subdirectory path")
			}
			return targetAbs, &it, nil
		}
	}
	return "", nil, fmt.Errorf("shared item not found")
}

func (sm *ServerManager) handleFiles(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	admin := sm.isAdmin(r, cfg)
	if !admin {
		if !cfg.ShareEnabled {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "File sharing is disabled"})
			return
		}
		if !sm.isClientAuthed(r, cfg) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "PIN authentication required"})
			return
		}
	}

	itemID := strings.TrimSpace(r.URL.Query().Get("item_id"))
	subPath := strings.TrimSpace(r.URL.Query().Get("sub_path"))
	categoryFilter := strings.TrimSpace(r.URL.Query().Get("category"))

	var entries []FileEntryView

	if itemID == "" {
		// Root view: list all items in TempDir + all explicitly shared Items
		_ = os.MkdirAll(cfg.TempDir, 0755)
		dirEntries, err := os.ReadDir(cfg.TempDir)
		if err == nil {
			for _, d := range dirEntries {
				if strings.HasPrefix(d.Name(), ".") {
					continue
				}
				info, err := d.Info()
				if err != nil {
					continue
				}
				sz := info.Size()
				if d.IsDir() {
					sz = CalculateDirSize(filepath.Join(cfg.TempDir, d.Name()))
				}
				cat := ClassifyFileCategory(d.Name(), d.IsDir())
				if categoryFilter != "" && categoryFilter != "all" && cat != categoryFilter {
					continue
				}
				entries = append(entries, FileEntryView{
					ID:        "__temp__",
					Name:      d.Name(),
					RelPath:   d.Name(),
					IsDir:     d.IsDir(),
					Size:      sz,
					Category:  cat,
					Mode:      "temp",
					UpdatedAt: info.ModTime(),
					Exists:    true,
				})
			}
		}

		// Also include configured ShareItems (skip those already inside TempDir root to avoid duplicates)
		tempAbs, _ := filepath.Abs(cfg.TempDir)
		for _, it := range cfg.Items {
			itAbs, _ := filepath.Abs(it.Path)
			if filepath.Dir(itAbs) == tempAbs {
				continue
			}
			info, err := os.Stat(it.Path)
			exists := err == nil
			sz := it.Size
			modTime := it.CreatedAt
			isDir := it.IsDir
			if exists {
				isDir = info.IsDir()
				modTime = info.ModTime()
				if isDir {
					sz = CalculateDirSize(it.Path)
				} else {
					sz = info.Size()
				}
			}
			cat := ClassifyFileCategory(it.Name, isDir)
			if categoryFilter != "" && categoryFilter != "all" && cat != categoryFilter {
				continue
			}
			entries = append(entries, FileEntryView{
				ID:        it.ID,
				Name:      it.Name,
				RelPath:   "",
				IsDir:     isDir,
				Size:      sz,
				Category:  cat,
				Mode:      it.Mode,
				UpdatedAt: modTime,
				Exists:    exists,
			})
		}
	} else {
		// Browsing inside a directory
		targetPath, rootItem, err := sm.resolveRootOrSub(cfg, itemID, subPath)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		dirEntries, err := os.ReadDir(targetPath)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unable to read directory: " + err.Error()})
			return
		}
		for _, d := range dirEntries {
			if strings.HasPrefix(d.Name(), ".") {
				continue
			}
			info, err := d.Info()
			if err != nil {
				continue
			}
			rel := d.Name()
			if subPath != "" {
				rel = filepath.ToSlash(filepath.Join(subPath, d.Name()))
			}
			sz := info.Size()
			if d.IsDir() {
				sz = CalculateDirSize(filepath.Join(targetPath, d.Name()))
			}
			cat := ClassifyFileCategory(d.Name(), d.IsDir())
			if categoryFilter != "" && categoryFilter != "all" && cat != categoryFilter {
				continue
			}
			entries = append(entries, FileEntryView{
				ID:        rootItem.ID,
				Name:      d.Name(),
				RelPath:   rel,
				IsDir:     d.IsDir(),
				Size:      sz,
				Category:  cat,
				Mode:      rootItem.Mode,
				UpdatedAt: info.ModTime(),
				Exists:    true,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
	})
}

func (sm *ServerManager) handleDeleteFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	cfg := sm.CM.GetSnapshot()
	if !sm.canManage(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Permission denied (read-only mode)"})
		return
	}

	var req struct {
		Targets []struct {
			ID      string `json:"id"`
			RelPath string `json:"rel_path"`
		} `json:"targets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
		return
	}

	removeIDs := make(map[string]bool)
	for _, t := range req.Targets {
		if t.ID == "__temp__" && t.RelPath != "" {
			fullPath, _, err := sm.resolveRootOrSub(cfg, "__temp__", t.RelPath)
			if err == nil {
				_ = os.RemoveAll(fullPath)
			}
			continue
		}
		if t.RelPath == "" {
			// Removing root shared item
			for _, it := range cfg.Items {
				if it.ID == t.ID {
					if it.Mode == "copy" {
						_ = os.RemoveAll(it.Path)
					}
					removeIDs[it.ID] = true
				}
			}
		} else {
			// Removing a sub-file inside a shared folder
			fullPath, rootIt, err := sm.resolveRootOrSub(cfg, t.ID, t.RelPath)
			if err == nil && rootIt != nil {
				_ = os.RemoveAll(fullPath)
			}
		}
	}

	if len(removeIDs) > 0 {
		_ = sm.CM.Update(func(c *Config) {
			var kept []ShareItem
			for _, it := range c.Items {
				if !removeIDs[it.ID] {
					kept = append(kept, it)
				}
			}
			c.Items = kept
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (sm *ServerManager) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}
	cfg := sm.CM.GetSnapshot()
	if !sm.canManage(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Read-only mode: client upload/edit permission is disabled"})
		return
	}

	// Support large uploads (up to 32MB in memory buffer, rest on disk)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Failed to parse multipart form: " + err.Error()})
		return
	}

	itemID := r.FormValue("item_id")
	subPath := r.FormValue("sub_path")
	targetDir := cfg.TempDir
	if itemID != "" {
		resolved, it, err := sm.resolveRootOrSub(cfg, itemID, subPath)
		if err == nil && it != nil && it.IsDir {
			targetDir = resolved
		}
	}
	_ = os.MkdirAll(targetDir, 0755)

	files := r.MultipartForm.File["files"]
	relPaths := r.MultipartForm.Value["rel_paths"]

	savedCount := 0
	for i, fh := range files {
		relName := filepath.Base(fh.Filename)
		if i < len(relPaths) && strings.TrimSpace(relPaths[i]) != "" {
			cleaned := filepath.Clean("/" + strings.TrimSpace(relPaths[i]))
			cleaned = strings.TrimPrefix(cleaned, "/")
			if cleaned != "" && cleaned != "." {
				relName = cleaned
			}
		}

		destPath := filepath.Join(targetDir, relName)
		destAbs, err := filepath.Abs(destPath)
		baseAbs, _ := filepath.Abs(targetDir)
		if err != nil || !strings.HasPrefix(destAbs, baseAbs) {
			continue
		}

		if filepath.Dir(destAbs) == baseAbs {
			destAbs = UniqueDestPath(baseAbs, filepath.Base(destAbs))
		} else {
			_ = os.MkdirAll(filepath.Dir(destAbs), 0755)
		}

		srcFile, err := fh.Open()
		if err != nil {
			continue
		}
		dstFile, err := os.Create(destAbs)
		if err != nil {
			_ = srcFile.Close()
			continue
		}
		_, _ = io.Copy(dstFile, srcFile)
		_ = dstFile.Close()
		_ = srcFile.Close()
		savedCount++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":    true,
		"count": savedCount,
	})
}

func (sm *ServerManager) handleDownload(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	if !sm.isAdmin(r, cfg) {
		if !cfg.ShareEnabled || !sm.isClientAuthed(r, cfg) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	itemID := r.URL.Query().Get("item_id")
	relPath := r.URL.Query().Get("rel_path")

	targetPath, _, err := sm.resolveRootOrSub(cfg, itemID, relPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if info.IsDir() {
		// Stream folder as a .zip archive
		zipName := info.Name() + ".zip"
		encodedName := url.PathEscape(zipName)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", zipName, encodedName))

		zw := zip.NewWriter(w)
		defer zw.Close()

		_ = filepath.WalkDir(targetPath, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			rel, err := filepath.Rel(targetPath, p)
			if err != nil {
				return nil
			}
			fw, err := zw.Create(filepath.ToSlash(rel))
			if err != nil {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			_, _ = io.Copy(fw, f)
			return nil
		})
		return
	}

	fileName := info.Name()
	encodedName := url.PathEscape(fileName)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", fileName, encodedName))
	http.ServeFile(w, r, targetPath)
}

func (sm *ServerManager) addLocalPathsToShare(paths []string) (int, error) {
	cfg := sm.CM.GetSnapshot()
	added := 0
	var newItems []ShareItem

	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}

		if cfg.ShareMode == "copy" {
			_ = os.MkdirAll(cfg.TempDir, 0755)
			dest := UniqueDestPath(cfg.TempDir, info.Name())
			if err := CopyFileOrDir(absPath, dest); err == nil {
				added++
			}
		} else {
			// "map" mode: zero-copy link
			exists := false
			for _, existing := range cfg.Items {
				if existing.Path == absPath {
					exists = true
					break
				}
			}
			if !exists {
				sz := info.Size()
				if info.IsDir() {
					sz = CalculateDirSize(absPath)
				}
				newItems = append(newItems, ShareItem{
					ID:        GenerateRandomHex(6),
					Name:      info.Name(),
					Path:      absPath,
					IsDir:     info.IsDir(),
					Size:      sz,
					Mode:      "map",
					CreatedAt: time.Now(),
				})
				added++
			}
		}
	}

	if len(newItems) > 0 {
		_ = sm.CM.Update(func(c *Config) {
			c.Items = append(newItems, c.Items...)
		})
	}
	return added, nil
}

func (sm *ServerManager) handlePCPick(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	if !sm.isAdmin(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Host admin permission required"})
		return
	}
	var req struct {
		Type string `json:"type"` // "file", "dir", "temp_dir"
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Type == "temp_dir" {
		paths, err := PickSystemPaths(true)
		if err != nil || len(paths) == 0 {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "cancelled": true})
			return
		}
		chosen := paths[0]
		_ = os.MkdirAll(chosen, 0755)
		_ = sm.CM.Update(func(c *Config) {
			c.TempDir = chosen
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "temp_dir": chosen})
		return
	}

	paths, err := PickSystemPaths(req.Type == "dir")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(paths) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "added": 0})
		return
	}
	added, _ := sm.addLocalPathsToShare(paths)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "added": added})
}

func (sm *ServerManager) handlePCAddPaths(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	if !sm.isAdmin(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Host admin permission required"})
		return
	}
	var req struct {
		Paths []string `json:"paths"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	added, _ := sm.addLocalPathsToShare(req.Paths)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "added": added})
}

func (sm *ServerManager) handlePCOpenFinder(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	if !sm.isAdmin(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Host admin permission required"})
		return
	}
	var req struct {
		ItemID  string `json:"item_id"`
		RelPath string `json:"rel_path"`
		TempRoot bool  `json:"temp_root"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.TempRoot {
		_ = os.MkdirAll(cfg.TempDir, 0755)
		_ = OpenInFileManager(cfg.TempDir, true)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
		return
	}

	targetPath, _, err := sm.resolveRootOrSub(cfg, req.ItemID, req.RelPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Local path does not exist"})
		return
	}
	_ = OpenInFileManager(targetPath, info.IsDir())
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (sm *ServerManager) handlePCConfig(w http.ResponseWriter, r *http.Request) {
	cfg := sm.CM.GetSnapshot()
	if !sm.isAdmin(r, cfg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Host admin permission required"})
		return
	}

	var req struct {
		Port            *int    `json:"port"`
		ShareEnabled    *bool   `json:"share_enabled"`
		Password        *string `json:"password"`
		RandomPassword  bool    `json:"random_password"`
		AllowClientEdit *bool   `json:"allow_client_edit"`
		ShareMode       *string `json:"share_mode"`
		TempDir         *string `json:"temp_dir"`
		AutoKeepAlive   *bool   `json:"auto_keep_alive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid config parameters"})
		return
	}

	portChanged := false
	newPort := cfg.Port

	_ = sm.CM.Update(func(c *Config) {
		if req.Port != nil && *req.Port > 0 && *req.Port <= 65535 && *req.Port != c.Port {
			c.Port = *req.Port
			newPort = *req.Port
			portChanged = true
		}
		if req.ShareEnabled != nil {
			c.ShareEnabled = *req.ShareEnabled
		}
		if req.RandomPassword {
			c.Password = Generate6DigitPassword()
			c.PasswordVersion = GenerateRandomHex(8)
		} else if req.Password != nil {
			pwd := strings.TrimSpace(*req.Password)
			if len(pwd) == 6 && pwd != c.Password {
				c.Password = pwd
				c.PasswordVersion = GenerateRandomHex(8)
			}
		}
		if req.AllowClientEdit != nil {
			c.AllowClientEdit = *req.AllowClientEdit
		}
		if req.ShareMode != nil && (*req.ShareMode == "map" || *req.ShareMode == "copy") {
			c.ShareMode = *req.ShareMode
		}
		if req.TempDir != nil && strings.TrimSpace(*req.TempDir) != "" {
			td := strings.TrimSpace(*req.TempDir)
			_ = os.MkdirAll(td, 0755)
			c.TempDir = td
		}
		if req.AutoKeepAlive != nil {
			c.AutoKeepAlive = *req.AutoKeepAlive
		}
	})

	updated := sm.CM.GetSnapshot()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"port_changed": portChanged,
		"new_port":     newPort,
		"admin_url":    fmt.Sprintf("http://127.0.0.1:%d/api/admin/entry?secret=%s", newPort, updated.AdminSecret),
	})

	if portChanged {
		go func() {
			time.Sleep(300 * time.Millisecond)
			sm.Restart()
		}()
	}
}
