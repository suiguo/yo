package logger_test

import (
	"context"
	"testing"

	"github.com/suiguo/yo/logger"
	"go.uber.org/zap"
)

func BenchmarkLogger_Info(b *testing.B) {
	log, _ := logger.InitLoggerFromFile("bench", "log.yaml", 1)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			log.Info("benchmark info", "key", "value")
		}
	})
}

func BenchmarkLogger_WithContextTrace(b *testing.B) {
	log, _ := logger.InitLoggerFromFile("bench", "log.yaml", 1)
	ctx := context.WithValue(context.Background(), logger.TraceIDKey, "bench-id")
	traceLog := log.WithContextTrace(ctx)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			traceLog.Info("benchmark with trace", "key", "value")
		}
	})
}

func BenchmarkZap_Info(b *testing.B) {
	log, _ := logger.InitLoggerFromFile("bench", "log.yaml", 1)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			log.Zap().Info("benchmark info", zap.String("key", "value"))
		}
	})
}
