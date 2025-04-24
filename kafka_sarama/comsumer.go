package kafkasarama

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ## 设计目标

// - 支持多个 topic 进行分别订阅
// - 各订阅保证独立性（互不应、无侧带效应）
// - 系统实现保持线性性，对用户 handler 无需同步而且性能可控
// - 防止 handler 等待时间过长导致系统阻塞
// - 支持持久运行、自恢复、不丢失消息

// ## 核心模型说明

// - 每个订阅计算为一个 session，通过 uuid 匹配 context
// - 每个 session 会创建一个 goroutine，读取接收到的 kafka message
// - 每条 message 传入 channel -> handler 线程 进行处理
// - handler 线程为单 goroutine，保证线性性
// - 默认 buffer size = 128
type GroupComsumer interface {
	SubTopics(topics []string, handler MessageHandler) (string, error)                                 // 订阅指定 topic 列表
	SubTopicsWithContext(ctx context.Context, topics []string, handler MessageHandler) (string, error) // 带上下文的订阅
	UnSub(session_id string)                                                                           // 取消指定 session_id 的订阅
	UnSubAll()                                                                                         // 取消所有订阅
	ListSubscriptions() map[string][]string
	Close() // 关闭消费者
}

// contextKey 是自定义 context key 类型
type contextKey string

// MessageHandler 是消息处理函数定义，返回 ack 表示是否确认消费
type MessageHandler func(topic string, msg *sarama.ConsumerMessage) (ack bool)

type saramSession struct {
	msg     *sarama.ConsumerMessage
	session sarama.ConsumerGroupSession
}

// sessionInfo 保存每个订阅 session 的信息
type sessionInfo struct {
	handler MessageHandler     // 用户注册的消息处理函数
	topics  []string           // 订阅的 topic 列表
	cancel  context.CancelFunc // 用于取消该 session 的 context
	msg     chan *saramSession
}

// groupConsumer 是 GroupComsumer 的实现，封装了 Kafka 消费组的逻辑
type groupConsumer struct {
	ctxUUIDKey contextKey           // 存放 session_id 的 context key
	globalCtx  context.Context      // 全局上下文
	cancel     context.CancelFunc   // 全局 context 的取消函数
	client     sarama.ConsumerGroup // Kafka 消费组客户端
	autoAck    bool                 // 是否自动提交 offset
	handlerMap sync.Map             // 存储所有 session_id -> sessionInfo
	groupID    string               // 消费组 ID
	logger     *zap.Logger          // 日志记录器
}

// NewGroupConsumer 创建一个新的 Kafka 消费组实例
func NewGroupConsumer(brokers []string, groupID string, cfg *sarama.Config, zap_log *zap.Logger) (GroupComsumer, error) {
	client, err := sarama.NewConsumerGroup(brokers, groupID, cfg)
	if err != nil {
		return nil, fmt.Errorf("new consumer group failed: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	// 启动后台协程处理 consumer group 的错误
	go func() {
		for {
			select {
			case err := <-client.Errors():
				if zap_log != nil {
					zap_log.Error("Consumer", zap.Error(err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return &groupConsumer{
		ctxUUIDKey: contextKey("uuid"),
		client:     client,
		groupID:    groupID,
		globalCtx:  ctx,
		cancel:     cancel,
		autoAck:    cfg.Consumer.Offsets.AutoCommit.Enable,
		logger:     zap_log,
	}, nil
}

// Close 关闭消费者，清理所有资源
func (gc *groupConsumer) Close() {
	gc.UnSubAll()
	gc.cancel()
	if err := gc.client.Close(); err != nil && gc.logger != nil {
		gc.logger.Error("failed to close consumer group", zap.Error(err))
	}
}

// UnSubAll 取消所有 session 的订阅
func (gc *groupConsumer) UnSubAll() {
	gc.handlerMap.Range(func(key, value any) bool {
		if s, ok := value.(*sessionInfo); ok {
			s.cancel()
		}
		gc.handlerMap.Delete(key)
		return true
	})
}

// SubTopicsWithContext 使用给定 context 订阅 topics，并注册 handler
// 为了保证稳定，handler 必须及时处理否则后续消息会阻塞
func (gc *groupConsumer) SubTopicsWithContext(ctx context.Context, topics []string, handler MessageHandler) (string, error) {
	if gc.client == nil {
		return "", errors.New("未初始化")
	}
	if handler == nil {
		return "", errors.New("未注册处理函数")
	}
	session_id := uuid.NewString()
	tmp := context.WithValue(ctx, gc.ctxUUIDKey, session_id)
	cctx, cancel := context.WithCancel(tmp)

	// 存储 session 信息
	session_info := &sessionInfo{
		handler: handler,
		cancel:  cancel,
		topics:  topics,
		msg:     make(chan *saramSession, 128),
	}
	gc.handlerMap.Store(session_id, session_info)

	// 启动后台协程消费消息
	go func() {
		for {
			select {
			case <-gc.globalCtx.Done():
				return
			case <-cctx.Done():
				return
			default:
				err := gc.client.Consume(cctx, topics, gc)
				// 正常退出：context 取消或 consumer group 被关闭
				if errors.Is(err, context.Canceled) || errors.Is(err, sarama.ErrClosedConsumerGroup) {
					if gc.logger != nil {
						gc.logger.Info("Consumer shutdown", zap.String("session_id", session_id), zap.Error(err))
					}
					gc.UnSub(session_id)
				}
				// 异常重试
				if gc.logger != nil {
					gc.logger.Error("Consumer error", zap.String("session_id", session_id), zap.Error(err))
				}
				time.Sleep(time.Second)
			}
		}
	}()
	go func() {
		for {
			select {
			case <-gc.globalCtx.Done():
				return
			case <-cctx.Done():
				return
			case data := <-session_info.msg:
				if session_info.handler(data.msg.Topic, data.msg) && !gc.autoAck {
					data.session.MarkMessage(data.msg, "")
				}
			}
		}
	}()
	return session_id, nil
}

// SubTopics 是简化版本的订阅，不带自定义 context
func (gc *groupConsumer) SubTopics(topics []string, handler MessageHandler) (string, error) {
	ctx := context.Background()
	return gc.SubTopicsWithContext(ctx, topics, handler)
}

// UnSub 取消指定 session_id 的订阅
func (gc *groupConsumer) UnSub(session_id string) {
	t, ok := gc.handlerMap.Load(session_id)
	if !ok || t == nil {
		return
	}
	if s, ok := t.(*sessionInfo); ok {
		gc.handlerMap.Delete(session_id)
		s.cancel()
	}
}

func (gc *groupConsumer) ListSubscriptions() map[string][]string {
	subs := make(map[string][]string)
	gc.handlerMap.Range(func(key, value any) bool {
		if s, ok := value.(*sessionInfo); ok {
			subs[key.(string)] = s.topics
		}
		return true
	})
	return subs
}

// Setup 是 sarama.ConsumerGroupHandler 接口的实现，消费前调用
func (gc *groupConsumer) Setup(_ sarama.ConsumerGroupSession) error { return nil }

// Cleanup 是 sarama.ConsumerGroupHandler 接口的实现，消费结束后调用
func (gc *groupConsumer) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim 是实际处理消息的逻辑
func (gc *groupConsumer) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	defer func() {
		if r := recover(); r != nil {
			if gc.logger != nil {
				gc.logger.Error("panic in ConsumeClaim", zap.Any("recover", r))
			}
		}
	}()

	// 获取当前 context 中的 session_id
	ctx := sess.Context()
	uuid := ctx.Value(gc.ctxUUIDKey)
	if uuid == nil {
		return nil
	}
	id, ok := uuid.(string)
	if !ok {
		return nil
	}

	t, ok := gc.handlerMap.Load(id)
	if !ok {
		return nil
	}
	info, ok := t.(*sessionInfo)
	if !ok || info == nil {
		return nil
	}
	for data := range claim.Messages() {
		if ctx.Err() != nil || gc.globalCtx.Err() != nil {
			return nil
		}
		// 立即判断 context 是否已取消
		select {
		case <-ctx.Done():
			return nil
		case <-gc.globalCtx.Done():
			return nil
		case info.msg <- &saramSession{msg: data, session: sess}:
		}
	}
	return nil
}
