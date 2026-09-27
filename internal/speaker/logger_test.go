package speaker

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestLogrusGoesToTheLog(t *testing.T) {
	var buf bytes.Buffer
	routeLogrus(slog.New(slog.NewTextHandler(&buf, nil)))
	logrus.Info("started audio-toolbox output")
	if !strings.Contains(buf.String(), "started audio-toolbox output") {
		t.Fatalf("logrus message not in the log: %q", buf.String())
	}
	if logrus.StandardLogger().Out != io.Discard {
		t.Fatal("logrus still writes to the terminal")
	}
}
