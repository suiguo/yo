package kafkatest

import (
	"testing"

	kafkasarama "github.com/suiguo/yo/kafka_sarama"
	"github.com/suiguo/yo/logger"
)

var l = logger.GetLogger("kafka")

func TestKafka(t *testing.T) {
	producer, err := kafkasarama.NewProducerAsyncFromSimpleFile("kafka.yaml", l.Zap())
	if err != nil {
		l.Panic("panice", "err", err)
	}
	producer.PushStringMessage("test", "hello")
	select {}
}
