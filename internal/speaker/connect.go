package speaker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/apresolve"
	"github.com/devgianlu/go-librespot/daemon"
	"github.com/devgianlu/go-librespot/spclient"
)

// Some contexts can't be started through go-librespot's API, which only
// takes a URI. The DJ needs the url of its session service too, which only
// Spotify's own apps send, in a Connect command. So the speaker sends itself
// that command, through Spotify, as a phone would.

// djSession is where the DJ's tracks and voiced intros come from.
const djSession = "hm://lexicon-session-provider/context-resolve/v2/session?contextUri="

// connect is a lazily made client for Spotify's internal API that talks as
// the speaker.
type connect struct {
	mu sync.Mutex
	sp *spclient.Spclient
}

func (s *Speaker) internalAPI(ctx context.Context) (*spclient.Spclient, error) {
	s.connect.mu.Lock()
	defer s.connect.mu.Unlock()
	if s.connect.sp != nil {
		return s.connect.sp, nil
	}
	addr, err := apresolve.NewApResolver(&librespot.NullLogger{}, http.DefaultClient, false).GetSpclient(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding Spotify's servers: %w", err)
	}
	sp, err := spclient.NewSpclient(ctx, &librespot.NullLogger{}, http.DefaultClient, addr, s.token, "", "")
	if err != nil {
		return nil, err
	}
	s.connect.sp = sp
	return sp, nil
}

// token is the speaker's own login, which Spotify's internal API accepts
// (the Web API token doesn't).
func (s *Speaker) token(ctx context.Context, _ bool) (string, error) {
	resp, err := s.request(ctx, daemon.ApiRequestTypeToken, nil)
	if err != nil {
		return "", err
	}
	tok, ok := resp.(*daemon.ApiToken)
	if !ok {
		return "", fmt.Errorf("unexpected token reply %T", resp)
	}
	return tok.Token, nil
}

// playSession starts uri, resolved through the session service at url, by
// sending the speaker a Connect play command. It starts at the track skipTo
// if the session has it, otherwise at the top.
func (s *Speaker) playSession(ctx context.Context, uri, url, skipTo string) error {
	st, err := s.status(ctx)
	if err != nil {
		return err
	}
	sp, err := s.internalAPI(ctx)
	if err != nil {
		return err
	}
	return s.sessionCommand(ctx, sp, st.DeviceId, st.DeviceId, uri, url, skipTo)
}

// sessionCommand sends a Connect play command for uri, resolved at url,
// from one device to another, through sp.
func (s *Speaker) sessionCommand(ctx context.Context, sp *spclient.Spclient, from, to, uri, url, skipTo string) error {
	if sp == nil {
		return errNoObserver
	}
	skip := map[string]any{}
	if skipTo != "" {
		skip["track_uri"] = skipTo
	}
	body, err := json.Marshal(map[string]any{"command": map[string]any{
		"endpoint":    "play",
		"context":     map[string]any{"uri": uri, "url": url, "metadata": map[string]string{}},
		"play_origin": map[string]any{"feature_identifier": "sptui"},
		"options":     map[string]any{"license": "on-demand", "skip_to": skip, "player_options_override": map[string]any{}},
	}})
	if err != nil {
		return err
	}
	resp, err := sp.Request(ctx, "POST", "/connect-state/v1/player/command/from/"+from+"/to/"+to, nil, nil, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("spotify refused to start %s: %d %s", uri, resp.StatusCode, msg)
	}
	return nil
}
