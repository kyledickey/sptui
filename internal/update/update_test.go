package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.0", "v0.2.0", false},
		{"v0.1.0", "v0.1.0", false},
		{"v1.0.0", "v0.9.9", true},
		{"v0.10.0", "v0.9.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "v1.0.0-rc.1", true},
		{"v1.0.0-rc.1", "v1.0.0", false},
		{"v1.0.0-rc.2", "v1.0.0-rc.1", true},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", true},
		{"v1.0.0-beta", "v1.0.0-alpha", true},
		{"v0.2.0", "dev", false},
		{"dev", "v0.1.0", false},
		{"0.2.0", "v0.1.0", false},
		{"v0.2", "v0.1.0", false},
	} {
		if got := Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// fakeGitHub serves a release like GitHub does: /releases/latest redirects
// to the tag, and the release's files download.
type fakeGitHub struct {
	latest   string
	files    map[string][]byte
	checks   atomic.Int32
	notFound bool
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prefix := "/" + Repo + "/releases/"
	switch {
	case r.URL.Path == prefix+"latest":
		f.checks.Add(1)
		if f.notFound {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "https://github.com/"+Repo+"/releases/tag/"+f.latest, http.StatusFound)
	case strings.HasPrefix(r.URL.Path, prefix+"download/"+f.latest+"/"):
		b, ok := f.files[strings.TrimPrefix(r.URL.Path, prefix+"download/"+f.latest+"/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	default:
		http.NotFound(w, r)
	}
}

func newUpdater(t *testing.T, gh *fakeGitHub) *Updater {
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	exe := filepath.Join(dir, "sptui")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Updater{
		Current:   "v0.1.0",
		Exe:       exe,
		CachePath: filepath.Join(dir, "cache", "update.json"),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		BaseURL:   srv.URL,
	}
}

func TestAvailableCachesForADay(t *testing.T) {
	gh := &fakeGitHub{latest: "v0.2.0"}
	u := newUpdater(t, gh)
	ctx := context.Background()
	for range 2 {
		got, err := u.Available(ctx, false)
		if err != nil || got != "v0.2.0" {
			t.Fatalf("Available = %q, %v; want v0.2.0", got, err)
		}
	}
	if n := gh.checks.Load(); n != 1 {
		t.Errorf("asked GitHub %d times, want once", n)
	}
	if _, err := u.Available(ctx, true); err != nil {
		t.Fatal(err)
	}
	if n := gh.checks.Load(); n != 2 {
		t.Errorf("fresh check didn't ask GitHub")
	}

	u.Current = "v0.2.0"
	if got, _ := u.Available(ctx, false); got != "" {
		t.Errorf("up to date, but Available = %q", got)
	}
}

func TestAvailableError(t *testing.T) {
	u := newUpdater(t, &fakeGitHub{notFound: true})
	if got, err := u.Available(context.Background(), false); err == nil {
		t.Fatalf("Available = %q, want an error", got)
	}
	if _, err := os.Stat(u.CachePath); err == nil {
		t.Error("a failed check was cached")
	}
}

// release builds a fake release whose sptui is a shell script printing
// version.
func release(t *testing.T, version string) map[string][]byte {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	script := []byte("#!/bin/sh\necho 'sptui " + version + "'\n")
	tw.WriteHeader(&tar.Header{Name: "sptui", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	tw.Write(script)
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	return map[string][]byte{
		ArchiveName():   buf.Bytes(),
		"checksums.txt": fmt.Appendf(nil, "%s  %s\n", hex.EncodeToString(sum[:]), ArchiveName()),
	}
}

func TestInstall(t *testing.T) {
	gh := &fakeGitHub{latest: "v0.2.0", files: release(t, "v0.2.0")}
	u := newUpdater(t, gh)
	if err := u.Install(context.Background(), "v0.2.0"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(u.Exe)
	if !strings.Contains(string(b), "sptui v0.2.0") {
		t.Errorf("binary wasn't replaced: %q", b)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(u.Exe), ".sptui-update-*"))
	if len(leftovers) > 0 {
		t.Errorf("left behind %v", leftovers)
	}
}

func TestInstallRefuses(t *testing.T) {
	for name, tamper := range map[string]func(files map[string][]byte){
		"bad checksum": func(f map[string][]byte) { f[ArchiveName()] = append(f[ArchiveName()], 0) },
		"no build":     func(f map[string][]byte) { f["checksums.txt"] = []byte("abc  sptui_plan9_mips.tar.gz\n") },
		"wrong version": func(f map[string][]byte) {
			other := release(t, "v0.1.5")
			f[ArchiveName()], f["checksums.txt"] = other[ArchiveName()], other["checksums.txt"]
		},
	} {
		t.Run(name, func(t *testing.T) {
			files := release(t, "v0.2.0")
			tamper(files)
			u := newUpdater(t, &fakeGitHub{latest: "v0.2.0", files: files})
			if err := u.Install(context.Background(), "v0.2.0"); err == nil {
				t.Fatal("installed anyway")
			}
			if b, _ := os.ReadFile(u.Exe); string(b) != "old" {
				t.Errorf("binary was replaced: %q", b)
			}
		})
	}
}

func TestInstallNotWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere")
	}
	u := newUpdater(t, &fakeGitHub{latest: "v0.2.0", files: release(t, "v0.2.0")})
	dir := filepath.Dir(u.Exe)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if err := u.Install(context.Background(), "v0.2.0"); !errors.Is(err, ErrNotWritable) {
		t.Fatalf("err = %v, want ErrNotWritable", err)
	}
}

func TestUnzip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("sptui.exe")
	w.Write([]byte("MZ binary"))
	zw.Close()
	got, err := unpack("sptui_windows_amd64.zip", buf.Bytes())
	if err != nil || string(got) != "MZ binary" {
		t.Fatalf("unpack = %q, %v", got, err)
	}
	if _, err := unpack("sptui_windows_amd64.zip", []byte("not a zip")); err == nil {
		t.Error("unpacked something that isn't a zip")
	}
}

// TestReplaceMovingAside replaces a binary the way it's done on Windows.
func TestReplaceMovingAside(t *testing.T) {
	dir := t.TempDir()
	exe, bin := filepath.Join(dir, "sptui.exe"), filepath.Join(dir, ".sptui-update-1.exe")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.WriteFile(exe+".old", []byte("older"), 0o755)
	os.WriteFile(bin, []byte("new"), 0o755)
	if err := replace(bin, exe, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("sptui.exe = %q, want new", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Errorf("sptui.exe.old = %q, want old", b)
	}
	if _, err := os.Stat(bin); err == nil {
		t.Error("the update is still there")
	}

	// A first install has nothing to move aside.
	os.Remove(exe)
	os.WriteFile(bin, []byte("newer"), 0o755)
	if err := replace(bin, exe, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "newer" {
		t.Errorf("sptui.exe = %q, want newer", b)
	}
}

func TestChecksum(t *testing.T) {
	sums := []byte("aaa  sptui_linux_amd64.tar.gz\nBBB *sptui_darwin_arm64.tar.gz\n")
	if got, ok := checksum(sums, "sptui_darwin_arm64.tar.gz"); !ok || got != "bbb" {
		t.Errorf("checksum = %q, %v", got, ok)
	}
	if _, ok := checksum(sums, "sptui_linux_arm64.tar.gz"); ok {
		t.Error("found a file that isn't there")
	}
}
