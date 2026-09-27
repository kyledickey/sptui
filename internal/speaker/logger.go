package speaker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/sirupsen/logrus"
)

// logger adapts slog to go-librespot's logrus-style Logger. Its chatty
// trace output is folded into debug.
type logger struct {
	log *slog.Logger
}

var _ librespot.Logger = (*logger)(nil)

func (l *logger) Tracef(format string, args ...any) { l.log.Debug(fmt.Sprintf(format, args...)) }
func (l *logger) Debugf(format string, args ...any) { l.log.Debug(fmt.Sprintf(format, args...)) }
func (l *logger) Infof(format string, args ...any)  { l.log.Info(fmt.Sprintf(format, args...)) }
func (l *logger) Warnf(format string, args ...any)  { l.log.Warn(fmt.Sprintf(format, args...)) }
func (l *logger) Errorf(format string, args ...any) { l.log.Error(fmt.Sprintf(format, args...)) }
func (l *logger) Trace(args ...any)                 { l.log.Debug(fmt.Sprint(args...)) }
func (l *logger) Debug(args ...any)                 { l.log.Debug(fmt.Sprint(args...)) }
func (l *logger) Info(args ...any)                  { l.log.Info(fmt.Sprint(args...)) }
func (l *logger) Warn(args ...any)                  { l.log.Warn(fmt.Sprint(args...)) }
func (l *logger) Error(args ...any)                 { l.log.Error(fmt.Sprint(args...)) }

func (l *logger) WithField(key string, value any) librespot.Logger {
	return &logger{log: l.log.With(key, value)}
}

func (l *logger) WithError(err error) librespot.Logger {
	return &logger{log: l.log.With("err", err)}
}

var routeOnce sync.Once

// routeLogrus sends logrus's global logger into log. Parts of go-librespot
// (the macOS audio driver, for one) write to it instead of the logger they
// are given, and it prints to the terminal, over the UI.
func routeLogrus(log *slog.Logger) {
	routeOnce.Do(func() {
		logrus.SetOutput(io.Discard)
		logrus.SetLevel(logrus.DebugLevel)
		logrus.AddHook(&logrusHook{log: log})
	})
}

type logrusHook struct{ log *slog.Logger }

func (h *logrusHook) Levels() []logrus.Level { return logrus.AllLevels }

func (h *logrusHook) Fire(e *logrus.Entry) error {
	level := slog.LevelDebug
	switch {
	case e.Level <= logrus.ErrorLevel:
		level = slog.LevelError
	case e.Level == logrus.WarnLevel:
		level = slog.LevelWarn
	case e.Level == logrus.InfoLevel:
		level = slog.LevelInfo
	}
	args := make([]any, 0, 2*len(e.Data))
	for k, v := range e.Data {
		args = append(args, k, v)
	}
	h.log.Log(context.Background(), level, e.Message, args...)
	return nil
}
