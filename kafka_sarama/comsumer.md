# KafkaSarama GroupConsumer

## 概览

`kafkasarama.GroupConsumer` 是对 Sarama 消费组接口的一封装，方便多个 topic 进行组织性的线性消费，提供最简单的 API 和系统稳定性。

## 设计目标

- 支持多个 topic 进行分别订阅
- 各订阅保证独立性（互不应、无侧带效应）
- 系统实现保持线性性，对用户 handler 无需同步而且性能可控
- 防止 handler 等待时间过长导致系统阻塞
- 支持持久运行、自恢复、不丢失消息

## 核心模型说明

- 每个订阅计算为一个 session，通过 uuid 匹配 context
- 每个 session 会创建一个 goroutine，读取接收到的 kafka message
- 每条 message 传入 channel -> handler 线程 进行处理
- handler 线程为单 goroutine，保证线性性
- 默认 buffer size = 64，防止系统内置阻塞

## 设计优势

- 无需项目为 handler 加锁，全线程线性
- 每个订阅独立 context ，断点维护方便
- goroutine 和 channel 中仅有一个级联，简单强大


## 简单示例

```go
id, _ := consumer.SubTopics([]string{"topic.a"}, func(topic string, msg *sarama.ConsumerMessage) bool {
    fmt.Println(string(msg.Value))
    return true // 确认消费
})

// 后续如果需要取消
consumer.UnSub(id)
```

---

若需要增加性能监控，持久 metrics，异常重试等功能，欢迎提出需求.

