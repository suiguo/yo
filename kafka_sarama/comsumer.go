package kafkasarama

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type MessageHandler func(topic string, msg *sarama.ConsumerMessage) (ack bool)

type GroupConsumer interface {
	SubTopics(topics []string, handler MessageHandler) (string, error)
	UnSub(sessionID string)
	ListSubscriptions() map[string][]string
	Close()
}
type cmsg struct {
	msg  *sarama.ConsumerMessage
	sess sarama.ConsumerGroupSession
}

type subEntry struct {
	id      string
	topics  []string
	handler MessageHandler
	msgCh   chan *cmsg
	cancel  context.CancelFunc
}

type groupConsumer struct {
	client  sarama.ConsumerGroup
	groupID string
	autoAck bool
	logger  *zap.Logger

	// 状态
	mu          sync.RWMutex
	runningCtx  context.Context
	runningStop context.CancelFunc
	closed      bool

	// 订阅索引
	// topic -> sessionID -> *subEntry
	subsByTopic map[string]map[string]*subEntry
	// sessionID -> *subEntry
	subsByID map[string]*subEntry
	// 提醒重启消费循环（topics 变更）
	reloadCh chan struct{}
	// 全局退出
	globalCtx  context.Context
	globalStop context.CancelFunc
}

func NewGroupConsumer(brokers []string, groupID string, cfg *sarama.Config, logger *zap.Logger) (GroupConsumer, error) {
	cg, err := sarama.NewConsumerGroup(brokers, groupID, cfg)
	if err != nil {
		return nil, fmt.Errorf("new consumer group failed: %w", err)
	}
	gctx, gcancel := context.WithCancel(context.Background())

	gc := &groupConsumer{
		client:      cg,
		groupID:     groupID,
		autoAck:     cfg.Consumer.Offsets.AutoCommit.Enable,
		logger:      logger,
		subsByTopic: make(map[string]map[string]*subEntry),
		subsByID:    make(map[string]*subEntry),
		reloadCh:    make(chan struct{}, 1),
		globalCtx:   gctx,
		globalStop:  gcancel,
	}

	// 统一错误日志
	go func() {
		for {
			select {
			case err := <-cg.Errors():
				if err != nil && logger != nil {
					logger.Error("consumer-group error", zap.Error(err))
				}
			case <-gctx.Done():
				return
			}
		}
	}()

	// 启动消费循环管理者
	go gc.runLoop()

	return gc, nil
}

func (gc *groupConsumer) SubTopics(topics []string, handler MessageHandler) (string, error) {
	if handler == nil {
		return "", errors.New("handler is nil")
	}
	gc.mu.Lock()
	defer gc.mu.Unlock()
	if gc.closed {
		return "", errors.New("consumer closed")
	}

	id := uuid.NewString()
	ctx, cancel := context.WithCancel(gc.globalCtx)
	entry := &subEntry{
		id:      id,
		topics:  dedup(topics),
		handler: handler,
		msgCh:   make(chan *cmsg, 1024),
		cancel:  cancel,
	}

	// 建立 topic 索引
	for _, t := range entry.topics {
		m, ok := gc.subsByTopic[t]
		if !ok {
			m = make(map[string]*subEntry)
			gc.subsByTopic[t] = m
		}
		m[id] = entry
	}
	gc.subsByID[id] = entry

	go func(e *subEntry) {
		defer func() {
			if r := recover(); r != nil && gc.logger != nil {
				gc.logger.Error("handler panic recovered", zap.Any("recover", r))
			}
		}()
		for {
			select {
			case <-gc.globalCtx.Done():
				return
			case <-ctx.Done():
				return
			case msg := <-e.msgCh:
				if msg == nil {
					return
				}
				func() {
					defer func() {
						if r := recover(); r != nil && gc.logger != nil {
							gc.logger.Error("handler panic", zap.Any("recover", r))
						}
					}()
					m := msg.msg
					if m != nil {
						ack := e.handler(m.Topic, m)
						if gc.autoAck {
							msg.sess.MarkMessage(m, "")
						} else {
							if ack && msg.sess != nil {
								if msg.sess.Context().Err() == nil {
									msg.sess.MarkMessage(m, "")
								}
							}
						}
					}

				}()
			}
		}
	}(entry)
	gc.requestReload()

	return id, nil
}

func (gc *groupConsumer) UnSub(sessionID string) {
	gc.mu.Lock()
	entry, ok := gc.subsByID[sessionID]
	if !ok {
		gc.mu.Unlock()
		return
	}
	delete(gc.subsByID, sessionID)
	for _, t := range entry.topics {
		if m, ok := gc.subsByTopic[t]; ok {
			delete(m, sessionID)
			if len(m) == 0 {
				delete(gc.subsByTopic, t)
			}
		}
	}
	gc.mu.Unlock()

	entry.cancel()
	gc.requestReload()
}

func (gc *groupConsumer) ListSubscriptions() map[string][]string {
	out := make(map[string][]string)
	gc.mu.RLock()
	defer gc.mu.RUnlock()
	for id, e := range gc.subsByID {
		out[id] = append([]string(nil), e.topics...)
	}
	return out
}

func (gc *groupConsumer) Close() {
	gc.mu.Lock()
	if gc.closed {
		gc.mu.Unlock()
		return
	}
	gc.closed = true
	// 清理所有 session
	for id, e := range gc.subsByID {
		e.cancel()
		delete(gc.subsByID, id)
	}
	gc.subsByTopic = make(map[string]map[string]*subEntry)
	// 停止全局
	gc.mu.Unlock()

	gc.globalStop()

	if err := gc.client.Close(); err != nil && gc.logger != nil {
		gc.logger.Error("close consumer-group", zap.Error(err))
	}
}

func (gc *groupConsumer) requestReload() {
	select {
	case gc.reloadCh <- struct{}{}:
	default:
	}
}

func (gc *groupConsumer) runLoop() {
	for {
		select {
		case <-gc.globalCtx.Done():
			return
		case <-gc.reloadCh:
			// 重启消费循环
			gc.restartConsume()
		}
	}
}

func (gc *groupConsumer) restartConsume() {
	// 关掉旧的 consume
	gc.mu.Lock()
	if gc.runningStop != nil {
		gc.runningStop()
		gc.runningStop = nil
	}
	// 汇总 topics
	topics := gc.unionAllTopicsLocked()
	// 若没有订阅，跳过
	if len(topics) == 0 {
		gc.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(gc.globalCtx)
	gc.runningCtx = ctx
	gc.runningStop = cancel
	gc.mu.Unlock()

	if gc.logger != nil {
		gc.logger.Info("consume restarting", zap.Strings("topics", topics))
	}

	// 起一个消费协程
	go func(ctx context.Context, topics []string) {
		for {
			if ctx.Err() != nil || gc.globalCtx.Err() != nil {
				return
			}
			err := gc.client.Consume(ctx, topics, gc)
			if errors.Is(err, context.Canceled) || errors.Is(err, sarama.ErrClosedConsumerGroup) {
				// 正常退出
				return
			}
			if err != nil && gc.logger != nil {
				gc.logger.Error("consume error; retrying", zap.Error(err))
			}
			time.Sleep(time.Second)
		}
	}(ctx, topics)
}

func (gc *groupConsumer) unionAllTopicsLocked() []string {
	set := map[string]struct{}{}
	for t := range gc.subsByTopic {
		set[t] = struct{}{}
	}
	var topics []string
	for t := range set {
		topics = append(topics, t)
	}
	sort.Strings(topics)
	return topics
}

func (gc *groupConsumer) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (gc *groupConsumer) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (gc *groupConsumer) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	defer func() {
		if r := recover(); r != nil && gc.logger != nil {
			gc.logger.Error("panic in ConsumeClaim", zap.Any("recover", r))
		}
	}()
	for msg := range claim.Messages() {
		if gc.globalCtx.Err() != nil {
			return nil
		}
		// 找到该 topic 下的所有订阅
		gc.mu.RLock()
		tmap := gc.subsByTopic[msg.Topic]
		// 快照
		var targets []*subEntry
		for _, e := range tmap {
			targets = append(targets, e)
		}
		gc.mu.RUnlock()
		for _, e := range targets {
			select {
			case e.msgCh <- &cmsg{msg: msg, sess: sess}:
			default:
				if gc.logger != nil {
					gc.logger.Warn("session channel full, drop msg",
						zap.String("topic", msg.Topic),
						zap.Int("len", len(e.msgCh)))
				}
			}
		}
	}
	return nil
}

// ---- utils ----

func dedup(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		m[s] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
