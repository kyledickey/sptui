package speaker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/apresolve"
	"github.com/devgianlu/go-librespot/dealer"
	connectpb "github.com/devgianlu/go-librespot/proto/spotify/connectstate"
	devicespb "github.com/devgianlu/go-librespot/proto/spotify/connectstate/devices"
	"github.com/devgianlu/go-librespot/spclient"
	"google.golang.org/protobuf/proto"
)

// The speaker only knows what it is playing itself. To see what the whole
// account is playing (a phone, the desktop app, another sptui), sptui keeps
// a second, hidden Connect device: an observer. Spotify pushes it the
// account's "cluster" (every device, which one is active, and its player
// state) whenever anything changes, so watching costs no Web API calls.

// observerRetry is how long the observer waits before reconnecting.
var observerRetry = 5 * time.Second

// observer holds the latest cluster Spotify pushed.
type observer struct {
	id string // the observer's own device ID, new each run

	mu      sync.Mutex
	cluster *connectpb.Cluster
	at      time.Time // when cluster arrived
	sp      *spclient.Spclient
	ready   chan struct{} // closed once the first cluster arrives
}

func newObserver() *observer {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return &observer{id: hex.EncodeToString(b), ready: make(chan struct{})}
}

// snapshot returns the latest cluster, or nil if none has arrived yet.
func (o *observer) snapshot() (*connectpb.Cluster, time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cluster, o.at
}

func (o *observer) set(c *connectpb.Cluster) {
	if c == nil {
		return
	}
	o.mu.Lock()
	first := o.cluster == nil
	o.cluster, o.at = c, time.Now()
	o.mu.Unlock()
	if first {
		close(o.ready)
	}
}

// client is the internal API client that talks as the observer, once it's
// connected.
func (o *observer) client() *spclient.Spclient {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sp
}

// watch keeps the observer connected until ctx is done.
func (s *Speaker) watch(ctx context.Context, log *slog.Logger) {
	for {
		err := s.observe(ctx, log)
		if ctx.Err() != nil {
			return
		}
		log.Debug("observer disconnected, retrying", "err", err)
		select {
		case <-time.After(observerRetry):
		case <-ctx.Done():
			return
		case <-s.done:
			return
		}
	}
}

// observe connects to Spotify's dealer, registers the observer and records
// every cluster pushed to it, until the connection drops.
func (s *Speaker) observe(ctx context.Context, log *slog.Logger) error {
	// The token comes from the speaker, which can't hand it over until it
	// has logged in.
	if _, err := s.token(ctx, false); err != nil {
		return err
	}
	resolver := apresolve.NewApResolver(&librespot.NullLogger{}, http.DefaultClient, false)
	dealerAddr, err := resolver.GetDealer(ctx)
	if err != nil {
		return fmt.Errorf("finding Spotify's dealer: %w", err)
	}
	spAddr, err := resolver.GetSpclient(ctx)
	if err != nil {
		return fmt.Errorf("finding Spotify's servers: %w", err)
	}
	sp, err := spclient.NewSpclient(ctx, &librespot.NullLogger{}, http.DefaultClient, spAddr, s.token, s.watcher.id, "")
	if err != nil {
		return err
	}

	d := dealer.NewDealer(&librespot.NullLogger{}, http.DefaultClient, dealerAddr, s.token)
	defer d.Close()
	if err := d.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to Spotify's dealer: %w", err)
	}
	msgs := d.ReceiveMessage("hm://pusher/v1/connections/", "hm://connect-state/v1/cluster")

	var connID string
	defer func() {
		if connID != "" {
			// Leave the account's device list tidy.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = sp.PutConnectStateInactive(ctx, connID, false)
		}
	}()
	for {
		var msg dealer.Message
		var ok bool
		select {
		case msg, ok = <-msgs:
			if !ok {
				return fmt.Errorf("dealer connection closed")
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
			return nil
		}
		switch {
		case strings.HasPrefix(msg.Uri, "hm://connect-state/v1/cluster"):
			var update connectpb.ClusterUpdate
			if err := proto.Unmarshal(msg.Payload, &update); err != nil {
				log.Debug("bad cluster update", "err", err)
				continue
			}
			s.watcher.set(update.Cluster)
			log.Debug("cluster update", "active", update.Cluster.GetActiveDeviceId(), "devices", clusterDevices(update.Cluster))
		default:
			// A new connection: (re-)register, which also returns the
			// cluster as it stands.
			connID = msg.Headers["Spotify-Connection-Id"]
			if connID == "" {
				continue
			}
			cluster, err := sp.PutConnectState(ctx, connID, s.watcher.registration())
			if err != nil {
				return fmt.Errorf("registering observer: %w", err)
			}
			s.watcher.mu.Lock()
			s.watcher.sp = sp
			s.watcher.mu.Unlock()
			s.watcher.set(cluster)
			log.Debug("observer registered", "id", s.watcher.id, "active", cluster.GetActiveDeviceId(), "devices", clusterDevices(cluster))
		}
	}
}

// clusterDevices describes a cluster's devices for the log, like
// "sptui (67d10549, hidden=false)".
func clusterDevices(c *connectpb.Cluster) []string {
	var out []string
	for id, d := range c.GetDevice() {
		out = append(out, fmt.Sprintf("%s (%.8s, hidden=%t)", d.GetName(), id, d.GetCapabilities().GetHidden()))
	}
	return out
}

// registration announces the observer: hidden, so it doesn't show in
// anyone's device list, and unable to play, so nothing transfers to it.
func (o *observer) registration() *connectpb.PutStateRequest {
	return &connectpb.PutStateRequest{
		ClientSideTimestamp: uint64(time.Now().UnixMilli()),
		MemberType:          connectpb.MemberType_CONNECT_STATE,
		PutStateReason:      connectpb.PutStateReason_NEW_DEVICE,
		IsActive:            false,
		Device: &connectpb.Device{
			DeviceInfo: &connectpb.DeviceInfo{
				CanPlay:               false,
				Name:                  "sptui remote",
				DeviceId:              o.id,
				DeviceType:            devicespb.DeviceType_COMPUTER,
				DeviceSoftwareVersion: librespot.VersionString(),
				ClientId:              librespot.ClientIdHex,
				SpircVersion:          "3.2.6",
				Brand:                 "spotify",
				Model:                 "sptui",
				Capabilities: &connectpb.Capabilities{
					CanBePlayer:            false,
					Hidden:                 true,
					IsObservable:           true,
					IsControllable:         false,
					SupportsGzipPushes:     true,
					SupportsCommandRequest: true,
					GaiaEqConnectId:        true,
				},
			},
			PlayerState: &connectpb.PlayerState{},
		},
	}
}
