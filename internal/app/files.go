package app

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxEditSize   = 2 << 20
	maxUploadSize = 512 << 20
)

func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

func (a *App) fileRoot() (string, error) {
	root := a.Sys.Path(WebRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

// resolve maps a user-supplied path to a real path inside the web root.
// Cleaning removes "..", and the deepest existing ancestor is resolved so a
// symlink inside the tree cannot lead outside it.
func (a *App) resolve(rel string) (string, error) {
	root, err := a.fileRoot()
	if err != nil {
		return "", err
	}
	if strings.ContainsRune(rel, 0) {
		return "", bad("invalid path")
	}
	p := filepath.Join(root, filepath.FromSlash(path.Clean("/"+rel)))
	for check := p; ; {
		realPath, err := filepath.EvalSymlinks(check)
		if err == nil {
			if !within(root, realPath) {
				return "", &HTTPError{http.StatusForbidden, "path is outside the web root"}
			}
			break
		}
		if !os.IsNotExist(err) {
			return "", bad("invalid path")
		}
		parent := filepath.Dir(check)
		if parent == check {
			break
		}
		check = parent
	}
	return p, nil
}

func (a *App) resolveNotRoot(rel string) (string, error) {
	p, err := a.resolve(rel)
	if err != nil {
		return "", err
	}
	if root, _ := a.fileRoot(); p == root {
		return "", bad("the web root itself cannot be changed")
	}
	return p, nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00") && len(name) <= 255
}

type fileEntry struct {
	Name     string `json:"name"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Mode     string `json:"mode"`
}

func (a *App) listFiles(w http.ResponseWriter, r *http.Request) error {
	rel := r.URL.Query().Get("path")
	p, err := a.resolve(rel)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return notFound("directory")
	}
	out := []fileEntry{}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, fileEntry{e.Name(), e.IsDir(), info.Size(), info.ModTime().Unix(), info.Mode().Perm().String()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, 200, map[string]any{"path": path.Clean("/" + rel), "entries": out})
	return nil
}

func (a *App) readFile(w http.ResponseWriter, r *http.Request) error {
	p, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return notFound("file")
	}
	if info.Size() > maxEditSize {
		return bad("file is too large to edit in the browser (limit 2 MB); download it instead")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return bad("this looks like a binary file; download it instead")
	}
	writeJSON(w, 200, map[string]string{"content": string(data)})
	return nil
}

func (a *App) writeFile(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := a.resolveNotRoot(in.Path)
	if err != nil {
		return err
	}
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return bad("that path is a directory")
	}
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		return bad("parent directory does not exist")
	}
	_, statErr := os.Stat(p)
	if err := os.WriteFile(p, []byte(in.Content), 0o644); err != nil {
		return err
	}
	if statErr != nil {
		a.Sys.ChownWeb(p)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) downloadFile(w http.ResponseWriter, r *http.Request) error {
	p, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return notFound("file")
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	// Always an attachment with a generic type, so uploaded HTML or SVG can
	// never execute in the panel's origin.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(info.Name()))
	http.ServeContent(w, r, "", info.ModTime(), f)
	return nil
}

func urlEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) error {
	dir, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return notFound("directory")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	mr, err := r.MultipartReader()
	if err != nil {
		return bad("expected a multipart upload")
	}
	count := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return bad("upload failed or exceeded the 512 MB limit")
		}
		name := filepath.Base(part.FileName())
		if part.FileName() == "" {
			continue
		}
		if !validName(name) {
			return bad("invalid file name")
		}
		dest, err := a.resolve(path.Join(r.URL.Query().Get("path"), name))
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, ".upload-*")
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tmp, part)
		tmp.Close()
		if copyErr != nil {
			os.Remove(tmp.Name())
			return bad("upload failed or exceeded the 512 MB limit")
		}
		_ = os.Chmod(tmp.Name(), 0o644)
		if err := os.Rename(tmp.Name(), dest); err != nil {
			os.Remove(tmp.Name())
			return bad("could not save %s", name)
		}
		a.Sys.ChownWeb(dest)
		count++
	}
	writeJSON(w, 200, map[string]int{"uploaded": count})
	return nil
}

func (a *App) mkdir(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !validName(in.Name) {
		return bad("invalid folder name")
	}
	p, err := a.resolve(path.Join(in.Path, in.Name))
	if err != nil {
		return err
	}
	if err := os.Mkdir(p, 0o755); err != nil {
		if os.IsExist(err) {
			return conflict("%s already exists", in.Name)
		}
		return bad("could not create folder")
	}
	a.Sys.ChownWeb(p)
	writeJSON(w, 201, map[string]bool{"ok": true})
	return nil
}

func (a *App) renameFile(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if !validName(in.Name) {
		return bad("invalid name")
	}
	src, err := a.resolveNotRoot(in.Path)
	if err != nil {
		return err
	}
	dst, err := a.resolve(path.Join(path.Dir(path.Clean("/"+in.Path)), in.Name))
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		return conflict("%s already exists", in.Name)
	}
	if err := os.Rename(src, dst); err != nil {
		return bad("could not rename")
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}

func (a *App) deleteFile(w http.ResponseWriter, r *http.Request) error {
	rel := r.URL.Query().Get("path")
	p, err := a.resolveNotRoot(rel)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(p); err != nil {
		return notFound("file")
	}
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	a.audit("file.deleted", path.Clean("/"+rel))
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
