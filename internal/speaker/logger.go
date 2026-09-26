package speaker

import (
	"fmt"
	"log/slog"

	librespot "github.com/devgianlu/go-librespot"
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
