package engine

import (
	"errors"
	_ "net/http/pprof"

	ilog "github.com/pion/ion-log"
	"github.com/sirupsen/logrus"
)

var (
	log *logrus.Logger
)

func init() {
	ilog.Init("info")
	log = ilog.NewLoggerWithFields(ilog.InfoLevel, "engine", nil)
}

func InitLog(level string) {
	ilog.Init(level)
	log = ilog.NewLoggerWithFields(ilog.StringToLevel(level), "engine", nil)
}

var (
	ErrorReplyNil      = errors.New("reply is nil")
	ErrorInvalidParams = errors.New("invalid params")
)
