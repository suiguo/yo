# logger

🚀 一个基于 [uber/zap](https://github.com/uber-go/zap) 的通用日志库，支持 traceId 自动传递、日志切割、多实例、链式调用，适用于中大型 Go 项目。

## ✨ 特性

- ✅ 支持多实例管理（按模块命名隔离）
- ✅ 支持 stdout / 文件日志 / lumberjack 文件切割
- ✅ 支持 traceId 上下文传递，适配分布式链路追踪
- ✅ 标准库 log.Println 自动接管进 zap
- ✅ 支持链式调用：WithContextTrace().Info(...)
- ✅ 高性能、高可读性、线程安全

## 📦 安装使用

## 🧱 初始化配置

```go
import "github.com/suiguo/yo/logger"

func main() {
	cfg := []*logger.LoggerCfg{
		{
			Name:       "./log/app.log",
			Level:      logger.InfoLevel,
			Maxsize:    100,
			Maxbackups: 7,
			Maxage:     30,
			Compress:   true,
		},
		{
			Name:  "stdout",
			Level: logger.DebugLevel,
		},
	}

	log, _ := logger.InitLogger("app", cfg, 1)
	log.RedirectStdLog()
}
```

## 📌 基本用法

```go
log := logger.GetLogger("app")
log.Info("服务启动", "port", 8080)
log.Error("请求失败", "err", err)
```

## 🔄 traceId 自动传递

```go
ctx := context.WithValue(context.Background(), logger.TraceIDKey, "trace-abc-123")
log.WithContextTrace(ctx).Info("用户登录成功", "userId", 42)
```
或者
```go
ctx := context.WithValue(context.Background(), logger.TraceIDKey, "trace-abc-123")
log.WithTrace("trace-abc-123").Info("用户登录成功", "userId", 42)
```

## 🌐 Gin 中间件支持示例

```go
func TraceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceId := c.GetHeader("X-Trace-Id")
		if traceId == "" {
			traceId = uuid.NewString()
		}
		ctx := context.WithValue(c.Request.Context(), logger.TraceIDKey, traceId)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
```


### 📊 Benchmark: `输入到文件`  
> **环境信息**  
> `goos: darwin`  
> `goarch: arm64`  
> `cpu: Apple M2`（8-core）

| Benchmark                           | Iterations | ns/op |
|------------------------------------|------------|--------|
| BenchmarkLogger_Info-8             | 1,504,573  | 3985   |
| BenchmarkLogger_Info-8             | 1,483,315  | 4019   |
| BenchmarkLogger_Info-8             | 1,504,963  | 4013   |
| BenchmarkLogger_WithContextTrace-8 | 1,436,314  | 4215   |
| BenchmarkLogger_WithContextTrace-8 | 1,428,747  | 4262   |
| BenchmarkLogger_WithContextTrace-8 | 1,434,648  | 4189   |
| BenchmarkZap_Info-8                | 1,543,862  | 3909   |
| BenchmarkZap_Info-8                | 1,512,843  | 3973   |
| BenchmarkZap_Info-8                | 1,523,089  | 3998   |


### 📊 Benchmark: `/dev/null`  
> **环境信息**  
> `goos: darwin`  
> `goarch: arm64`  
> `cpu: Apple M2`（8-core）

| Benchmark                            | Iterations  | ns/op   |
|-------------------------------------|-------------|---------|
| BenchmarkLogger_Info-8              | 17,947,256  | 345.1   |
| BenchmarkLogger_Info-8              | 15,693,417  | 339.8   |
| BenchmarkLogger_Info-8              | 17,667,505  | 343.3   |
| BenchmarkLogger_WithContextTrace-8  | 14,660,196  | 422.1   |
| BenchmarkLogger_WithContextTrace-8  | 12,922,285  | 418.8   |
| BenchmarkLogger_WithContextTrace-8  | 13,956,817  | 418.9   |
| BenchmarkZap_Info-8                 | 16,658,756  | 317.6   |
| BenchmarkZap_Info-8                 | 18,794,659  | 325.2   |
| BenchmarkZap_Info-8                 | 18,865,141  | 327.0   |
## 📎 License

MIT © 2025 Yoyyo
