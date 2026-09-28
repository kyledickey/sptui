// Package update finds out whether a newer sptui has been released on
// GitHub, and installs it over the running binary, the way install.sh does.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kyledickey/sptui/internal/atomicfile"
)

const (
	// Repo is where sptui's releases are published.
	Repo = "kyledickey/sptui"
	// checkEvery is how long a check's answer is trusted before asking
	// GitHub again.
	checkEvery = 24 * time.Hour
	// maxArchive guards against downloading something absurd.
	maxArchive = 256 << 20
)

// Updater checks for and installs releases of one running sptui.
type Updater struct {
	// Current is the running version, like "v0.1.0". It must be a release
	// (see IsRelease); development builds are never updated.
	Current string
	// Exe is the binary to replace. It must be the resolved path, not a
	// symlink to it; see Executable.
	Exe string
	// CachePath remembers the last check, so it happens at most once a day.
	CachePath string
	Log       *slog.Logger

	// For tests: the GitHub to talk to, and the HTTP client to do it with.
	BaseURL string
	Client  *http.Client
}

// Executable returns the path of the running binary with symlinks
// resolved, so an update replaces the file rather than the link.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func (u *Updater) base() string { return cmp.Or(u.BaseURL, "https://github.com") }

func (u *Updater) client() *http.Client {
	if u.Client != nil {
		return u.Client
	}
	return http.DefaultClient
}

// cached is what CachePath holds.
type cached struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

// Available reports a release newer than the running one, or "" if there
// isn't one. It asks GitHub at most once a day unless fresh is set.
func (u *Updater) Available(ctx context.Context, fresh bool) (string, error) {
	latest, err := u.latest(ctx, fresh)
	if err != nil || !Newer(latest, u.Current) {
		return "", err
	}
	return latest, nil
}

// latest returns the newest release's tag.
func (u *Updater) latest(ctx context.Context, fresh bool) (string, error) {
	if !fresh && u.CachePath != "" {
		var c cached
		if b, err := os.ReadFile(u.CachePath); err == nil && json.Unmarshal(b, &c) == nil &&
			time.Since(c.Checked) < checkEvery && c.Latest != "" {
			return c.Latest, nil
		}
	}
	tag, err := u.fetchLatest(ctx)
	if err != nil {
		return "", err
	}
	u.Log.Info("checked for updates", "latest", tag, "running", u.Current)
	if u.CachePath != "" {
		b, _ := json.Marshal(cached{Checked: time.Now(), Latest: tag})
		if err := os.MkdirAll(filepath.Dir(u.CachePath), 0o755); err == nil {
			if err := atomicfile.Write(u.CachePath, b, 0o644); err != nil {
				u.Log.Warn("couldn't save update check", "err", err)
			}
		}
	}
	return tag, nil
}

// fetchLatest asks GitHub for the newest release. Rather than the API,
// which allows 60 requests an hour, it reads where /releases/latest
// redirects to: .../releases/tag/v1.2.3.
func (u *Updater) fetchLatest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.base()+"/"+Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	client := *u.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("check for updates: %w", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	tag := path.Base(loc)
	if resp.StatusCode/100 != 3 || !strings.Contains(loc, "/releases/tag/") || !IsRelease(tag) {
		return "", fmt.Errorf("check for updates: GitHub answered %s", resp.Status)
	}
	return tag, nil
}

// ArchiveName is the release file for this machine.
func ArchiveName() string {
	return fmt.Sprintf("sptui_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// ErrNotWritable means the binary is somewhere the user can't write to,
// like a root-owned /usr/local/bin.
var ErrNotWritable = errors.New("not writable")

// Install downloads release tag, checks it against the release's
// checksums, makes sure it runs, and puts it in place of Exe. A running
// sptui keeps the binary it started with until it restarts.
func (u *Updater) Install(ctx context.Context, tag string) error {
	if !IsRelease(tag) {
		return fmt.Errorf("%q isn't a release", tag)
	}
	dir := filepath.Dir(u.Exe)
	// Fail before downloading anything if the binary can't be replaced.
	tmp, err := os.CreateTemp(dir, ".sptui-update-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("can't write to %s: %w", dir, ErrNotWritable)
		}
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once it's renamed into place
	defer tmp.Close()

	name := ArchiveName()
	u.Log.Info("installing update", "version", tag, "archive", name, "exe", u.Exe)
	sums, err := u.download(ctx, tag, "checksums.txt")
	if err != nil {
		return err
	}
	want, ok := checksum(sums, name)
	if !ok {
		return fmt.Errorf("release %s has no build for this computer (%s/%s)", tag, runtime.GOOS, runtime.GOARCH)
	}
	archive, err := u.download(ctx, tag, name)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s doesn't match its checksum; not installing it", name)
	}
	bin, err := unpack(archive)
	if err != nil {
		return fmt.Errorf("unpack %s: %w", name, err)
	}
	if _, err := tmp.Write(bin); err != nil {
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Make sure it runs, and is what it says it is, before it replaces the
	// binary that works.
	vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, tmp.Name(), "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the new sptui doesn't run: %w: %s", err, bytes.TrimSpace(out))
	}
	if got := strings.TrimSpace(string(out)); got != "sptui "+tag {
		return fmt.Errorf("the new sptui says it's %q, not %s", got, tag)
	}
	// Rename rather than overwrite: the running binary keeps its file, and
	// macOS doesn't mistake the new one for a tampered copy of the old.
	if err := os.Rename(tmp.Name(), u.Exe); err != nil {
		return err
	}
	u.Log.Info("installed update", "version", tag)
	return nil
}

// download fetches a file of release tag.
func (u *Updater) download(ctx context.Context, tag, file string) ([]byte, error) {
	url := u.base() + "/" + Repo + "/releases/download/" + tag + "/" + file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", file, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", file, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxArchive+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", file, err)
	}
	if len(b) > maxArchive {
		return nil, fmt.Errorf("download %s: too big", file)
	}
	return b, nil
}

// checksum finds file's SHA-256 in sha256sum's output.
func checksum(sums []byte, file string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		sum, name, ok := strings.Cut(sc.Text(), " ")
		name = strings.TrimLeft(name, " *") // " name" in text mode, "*name" in binary
		if ok && name == file {
			return strings.ToLower(sum), true
		}
	}
	return "", false
}

// unpack returns the sptui binary from a release archive.
func unpack(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("no sptui in it")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == "sptui" {
			return io.ReadAll(io.LimitReader(tr, maxArchive))
		}
	}
}

// IsRelease reports whether v is a release version like v1.2.3 (or
// v1.2.3-beta.1), as opposed to a development build.
func IsRelease(v string) bool {
	_, ok := parse(v)
	return ok
}

// Newer reports whether version a is newer than b. Anything that isn't a
// release is never newer, and never older.
func Newer(a, b string) bool {
	va, okA := parse(a)
	vb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range 3 {
		if va.nums[i] != vb.nums[i] {
			return va.nums[i] > vb.nums[i]
		}
	}
	// A release is newer than its prereleases.
	switch {
	case va.pre == vb.pre:
		return false
	case va.pre == "":
		return true
	case vb.pre == "":
		return false
	}
	return comparePre(va.pre, vb.pre) > 0
}

type version struct {
	nums [3]int
	pre  string
}

func parse(v string) (version, bool) {
	var out version
	rest, ok := strings.CutPrefix(v, "v")
	if !ok {
		return out, false
	}
	rest, out.pre, _ = strings.Cut(rest, "-")
	parts := strings.Split(rest, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out.nums[i] = n
	}
	return out, true
}

// comparePre orders prerelease tags the semver way: dot-separated parts,
// numbers numerically and below words.
func comparePre(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(pa), len(pb)) {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		switch {
		case errA == nil && errB == nil:
			if c := cmp.Compare(na, nb); c != 0 {
				return c
			}
		case errA == nil:
			return -1
		case errB == nil:
			return 1
		default:
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
		}
	}
	return cmp.Compare(len(pa), len(pb))
}
