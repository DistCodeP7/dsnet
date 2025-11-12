package controller

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"time"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

type NoOpLogger struct{}

func (n *NoOpLogger) Printf(format string, v ...interface{}) {}

type StoredMessage struct {
	Id      int
	Payload []byte
	From    string
	To      string
	Group   string
	Time    int64
}

var persistCh = make(chan StoredMessage, 10000)

func StartDiskWriter(filePath string) {
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	ticker := time.NewTicker(5 * time.Second)

	batch := []StoredMessage{}

	for {
		select {
		case msg := <-persistCh:
			batch = append(batch, msg)
			if len(batch) >= 500 {
				flushBatch(writer, batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				flushBatch(writer, batch)
				batch = []StoredMessage{}
			}
		}
	}
}

func flushBatch(writer *bufio.Writer, messages []StoredMessage) {
	for _, msg := range messages {
		data, _ := json.Marshal(msg)
		writer.Write(data)
		writer.WriteByte('\n')
	}
	writer.Flush()
}
