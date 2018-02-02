package simplereqresp

import (
	"log"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

type stdLogger struct{}

func (l stdLogger) Printf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

const (
	Helpful     = 1
	Verbose     = 2
	VeryVerbose = 3
)
