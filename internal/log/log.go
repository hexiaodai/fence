package log

import (
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

func NewLogger() (logr.Logger, error) {
	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		return logr.Logger{}, err
	}
	return zapr.NewLogger(zapLogger), nil
}
