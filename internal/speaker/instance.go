package speaker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devgianlu/go-librespot/daemon"
)

// One computer has one speaker, however many sptuis are open. The first
// sptui runs it and takes requests from the others on a socket beside its
// state file; the others are clients of it, so every window controls the
// same speaker. When the sptui running the speaker quits, one of the others
// takes it over (with the same device ID, so Spotify sees the same
// speaker).

// maxSocketPath is the longest socket path every platform takes (macOS
// allows 104 bytes, counting the terminating NUL).
const maxSocketPath = 103

// socketPath is where the speaker takes requests from other sptuis: beside
// its state file, or, if that path is too long for a socket, in the temp
// directory under a name made from it.
func socketPath(statePath string) string {
	path := strings.TrimSuffix(statePath, filepath.Ext(statePath)) + ".sock"
	if len(path) <= maxSocketPath {
		return path
	}
	sum := sha256.Sum256([]byte(statePath))
	return filepath.Join(os.TempDir(), fmt.Sprintf("sptui-%d-%x.sock", os.Getuid(), sum[:6]))
}

type wireRequest struct {
	Type daemon.ApiRequestType `json:"type"`
	Data json.RawMessage       `json:"data,omitempty"`
}

type wireReply struct {
	Data     json.RawMessage `json:"data,omitempty"`
	Err      string          `json:"err,omitempty"`
	NotReady bool            `json:"not_ready,omitempty"`
}

// serve answers other sptuis' requests until ctx is done.
func (s *Speaker) serve(ctx context.Context, log *slog.Logger) error {
	// Any socket already there is left over: whoever made it has given up
	// the lock this sptui now holds.
	_ = os.Remove(s.sock)
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "unix", s.sock)
	if err != nil {
		return err
	}
	// Only this user's sptuis may drive the speaker.
	if err := os.Chmod(s.sock, 0o600); err != nil {
		_ = l.Close()
		return err
	}
	context.AfterFunc(ctx, func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				if ctx.Err() == nil {
					log.Warn("other sptuis can no longer reach the speaker", "err", err)
				}
				return
			}
			go s.answer(ctx, c)
		}
	}()
	return nil
}

// answer handles one request from another sptui.
func (s *Speaker) answer(ctx context.Context, c net.Conn) {
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var req wireRequest
	if err := json.NewDecoder(c).Decode(&req); err != nil {
		return
	}
	var reply wireReply
	data, err := decodeRequest(req.Type, req.Data)
	var resp any
	if err == nil {
		resp, err = s.local(ctx, req.Type, data)
	}
	switch {
	case errors.Is(err, errNotReady):
		reply.NotReady = true
	case err != nil:
		reply.Err = err.Error()
	case resp != nil:
		reply.Data, _ = json.Marshal(resp)
	}
	_ = json.NewEncoder(c).Encode(reply)
}

// forward sends a request to the speaker another sptui runs.
func (s *Speaker) forward(ctx context.Context, typ daemon.ApiRequestType, data any) (any, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", s.sock)
	if err != nil {
		// That sptui has quit, and this one is about to take over.
		return nil, errNotReady
	}
	defer func() { _ = c.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = c.SetDeadline(time.Now()) })
	defer stop()

	req := wireRequest{Type: typ}
	if data != nil {
		if req.Data, err = json.Marshal(data); err != nil {
			return nil, err
		}
	}
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return nil, errNotReady
	}
	var reply wireReply
	if err := json.NewDecoder(c).Decode(&reply); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errNotReady
	}
	switch {
	case reply.NotReady:
		return nil, errNotReady
	case reply.Err != "":
		return nil, errors.New(reply.Err)
	}
	return decodeReply(typ, reply.Data)
}

// decodeRequest rebuilds a request's data as the player expects it.
func decodeRequest(typ daemon.ApiRequestType, raw json.RawMessage) (any, error) {
	switch typ {
	case daemon.ApiRequestTypePlay:
		return decode[daemon.ApiPlay](raw)
	case daemon.ApiRequestTypeSeek:
		return decode[daemon.ApiSeek](raw)
	case daemon.ApiRequestTypeSetVolume:
		return decode[daemon.ApiSetVolume](raw)
	case daemon.ApiRequestTypeNext:
		return decode[daemon.ApiNext](raw)
	case daemon.ApiRequestTypeSetShufflingContext, daemon.ApiRequestTypeSetRepeatingContext, daemon.ApiRequestTypeSetRepeatingTrack:
		return decode[bool](raw)
	case daemon.ApiRequestTypeAddToQueue:
		return decode[string](raw)
	}
	return nil, nil
}

// decodeReply rebuilds a reply as the player gives it.
func decodeReply(typ daemon.ApiRequestType, raw json.RawMessage) (any, error) {
	switch typ {
	case daemon.ApiRequestTypeStatus:
		st, err := decode[daemon.ApiStatus](raw)
		return &st, err
	case daemon.ApiRequestTypeToken:
		tok, err := decode[daemon.ApiToken](raw)
		return &tok, err
	}
	return nil, nil
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}
