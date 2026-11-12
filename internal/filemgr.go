package internal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileEntryView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	RelPath   string    `json:"rel_path"`
	IsDir     bool      `json:"is_dir"`
	Size      int64     `json:"size"`
	Category  string    `json:"category"` // dir, image, video, audio, doc, archive, code, other
	Mode      string    `json:"mode"`     // map, copy, temp
	UpdatedAt time.Time `json:"updated_at"`
	Exists    bool      `json:"exists"`
}

func ClassifyFileCategory(name string, isDir bool) string {
	if isDir {
		return "dir"
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".bmp", ".ico", ".heic":
		return "image"
	case ".mp4", ".mkv", ".mov", ".avi", ".webm", ".flv", ".wmv", ".m4v":
		return "video"
	case ".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a":
		return "audio"
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".md", ".csv", ".rtf":
		return "doc"
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz", ".dmg", ".iso", ".exe", ".msi", ".apk", ".pkg", ".deb", ".rpm":
		return "archive"
	case ".go", ".js", ".ts", ".py", ".java", ".c", ".cpp", ".h", ".html", ".css", ".json", ".yaml", ".yml", ".sh", ".sql", ".php", ".rs", ".vue":
		return "code"
	default:
		return "other"
	}
}

func CalculateDirSize(path string) int64 {
	var size int64
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	for _, d := range entries {
		if d.IsDir() {
			continue
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
		}
	}
	return size
}

func CopyFileOrDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func UniqueDestPath(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	candidate := filepath.Join(dir, name)
	idx := 1
	for {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s_(%d)%s", base, idx, ext))
		idx++
	}
}
