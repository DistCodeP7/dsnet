package controller

type Logger interface {
	Printf(format string, v ...interface{})
}

type NoOpLogger struct{}

func (n *NoOpLogger) Printf(format string, v ...interface{}) {}
