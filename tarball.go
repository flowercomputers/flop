package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRoot returns the toplevel of the git repository containing dir.
func gitRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", errors.New("not inside a git repository; git determines which files are uploaded")
	}
	return strings.TrimSpace(string(out)), nil
}

// gitMeta returns the short HEAD sha ("" if there are no commits) and whether
// the worktree has uncommitted changes.
func gitMeta(root string) (sha string, dirty bool) {
	if out, err := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD").Output(); err == nil {
		sha = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
		dirty = true
	}
	return sha, dirty
}

// sourceTarball archives the repo's tracked plus untracked-but-not-ignored
// files into an in-memory gzipped tar, paths relative to root. Uncommitted
// edits ship as they are on disk; gitignore handling is delegated to git.
func sourceTarball(root string) ([]byte, error) {
	out, err := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		full := filepath.Join(root, rel)
		// --cached also lists files deleted from the worktree but not yet
		// committed as deletions; skip those and anything not a regular file
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		hdr := &tar.Header{
			Name:    filepath.ToSlash(rel),
			Mode:    int64(info.Mode().Perm()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		f, err := os.Open(full)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(tw, f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("archive %s: %w", rel, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
