# Gendure

<img src="https://raw.githubusercontent.com/Marincor/assets/refs/heads/main/gendure.png" alt="Gendure Logo" width="100"/>

**Gendure** is a Go library providing resilience patterns to make your services more robust and fault-tolerant. Built to endure failures with battle-tested patterns like Circuit Breaker and Exponential Backoff Retry.

[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.18-blue)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![made-with-Go](https://img.shields.io/badge/Made%20with-Go-1f425f.svg)](https://go.dev/)
[![GoDoc reference example](https://img.shields.io/badge/godoc-reference-blue.svg)](https://godoc.org/github.com/marincor/gendure)
[![GoReportCard example](https://goreportcard.com/badge/github.com/nanomsg/mangos)](https://goreportcard.com/report/github.com/marincor/gendure)

## Features

- 🔌 **Circuit Breaker** - Prevent cascading failures by blocking requests to failing services
- 🔄 **Exponential Backoff Retry** - Retry failed operations with intelligent delay strategies
- 🎲 **Jitter Support** - Prevent thundering herd problems with randomized delays
- 🧵 **Thread-Safe** - Safe for concurrent use across multiple goroutines
- 📊 **Context-Aware** - Respect context cancellation and timeouts
- 🔍 **Observable** - Built-in logging support for monitoring and debugging
- 🎯 **Generic Types** - Type-safe implementations using Go generics

## Installation

```bash
go get github.com/marincor/gendure
```

## Quick Start

### Circuit Breaker

Protect your services from cascading failures by automatically opening the circuit when error thresholds are reached.

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/marincor/gendure"
)

func main() {
    // Create a circuit breaker for string responses
    cb := gendure.NewCircuitBreaker[string](
        3,                    // failure threshold
        30*time.Second,       // recovery timeout
        nil,                  // optional logger
    )
    
    ctx := context.Background()
    
    result, err := cb.Execute(
        ctx,
        // Primary operation
        func() (string, error) {
            return callExternalAPI()
        },
        // Fallback operation
        func() (string, error) {
            return "cached response", nil
        },
    )
    
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    }
    fmt.Printf("Result: %s\n", result)
}
```

### Exponential Backoff Retry

Retry failed operations with exponentially increasing delays and jitter to avoid overwhelming recovering services.

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/marincor/gendure"
)

func main() {
    retry := gendure.NewExponentialBackoffRetry[string](
        func() (string, error) {
            return callUnreliableService()
        },
        100*time.Millisecond, // initial delay
        5,                     // max retries
        2,                     // multiplier (2^attempt)
        3,                     // jitter up to 3 seconds
        nil,                   // optional logger
    )
    
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    result, err := retry.Execute(ctx)
    if err != nil {
        fmt.Printf("All retries failed: %v\n", err)
        return
    }
    
    fmt.Printf("Success: %s\n", result)
}
```

## Documentation

### Circuit Breaker

The Circuit Breaker pattern prevents cascading failures by monitoring error rates and temporarily blocking requests when thresholds are exceeded.

#### States

- **Closed**: Normal operation, requests pass through
- **Open**: Failure threshold exceeded, requests are blocked and fallback is used
- **Half-Open**: Testing if service recovered, allows one request

#### API

```go
// Create a new circuit breaker
func NewCircuitBreaker[T any](
    failureThreshold int32,     // failures before opening (default: 1)
    recoveryTimeout time.Duration, // wait before testing recovery (default: 30s)
    logger glogger.GLogger,      // optional logger
) *circuitBreaker[T]

// Execute with circuit breaker protection
func (cb *circuitBreaker[T]) Execute(
    ctx context.Context,
    operation func() (T, error),  // primary operation
    fallback func() (T, error),   // fallback when circuit is open
) (T, error)

// Get current state
func (cb *circuitBreaker[T]) GetState() int32

// Get current failure count
func (cb *circuitBreaker[T]) GetCountFailure() int32

// Manually reset the circuit breaker
func (cb *circuitBreaker[T]) Reset()
```

#### Example: HTTP Client with Circuit Breaker

```go
type APIClient struct {
    cb *gendure.CircuitBreaker[*http.Response]
}

func NewAPIClient() *APIClient {
    return &APIClient{
        cb: gendure.NewCircuitBreaker[*http.Response](
            5,                // open after 5 failures
            1*time.Minute,    // try recovery after 1 minute
            nil,
        ),
    }
}

func (c *APIClient) Get(ctx context.Context, url string) (*http.Response, error) {
    return c.cb.Execute(
        ctx,
        func() (*http.Response, error) {
            return http.Get(url)
        },
        func() (*http.Response, error) {
            // Return cached response or error
            return nil, errors.New("circuit open: service unavailable")
        },
    )
}
```

### Exponential Backoff Retry

The Exponential Backoff Retry pattern retries failed operations with exponentially increasing delays, plus random jitter to prevent thundering herd.

#### Delay Calculation

```
totalDelay = initialDelay × (multiplier^attempt) + randomJitter
```

Where `randomJitter` is a random value between 0 and `randomInt-1` seconds.

#### API

```go
// Create a new exponential backoff retry
func NewExponentialBackoffRetry[T any](
    callback CallbackFunc[T],      // operation to retry
    initialDelay time.Duration,    // base delay (default: 100ms)
    maxRetries int,                // max attempts (default: 3)
    multiplier int,                // growth factor (default: 2)
    randomInt int,                 // jitter range in seconds (default: 1)
    logger glogger.GLogger,        // optional logger
) ExponentialBackoffRetry[T]

// Execute with retry logic
func (ebr ExponentialBackoffRetry[T]) Execute(
    ctx context.Context,
) (T, error)
```

#### Example: Database Connection with Retry

```go
func connectToDatabase(ctx context.Context) (*sql.DB, error) {
    retry := gendure.NewExponentialBackoffRetry[*sql.DB](
        func() (*sql.DB, error) {
            return sql.Open("postgres", connectionString)
        },
        500*time.Millisecond, // start with 500ms
        5,                     // try 5 times
        2,                     // double delay each time
        2,                     // add 0-1s jitter
        nil,
    )
    
    return retry.Execute(ctx)
}
```

#### Retry Timeline Example

With `initialDelay=100ms`, `multiplier=2`, `maxRetries=5`, `jitter=0-2s`:

| Attempt | Base Delay | Jitter | Total Delay |
|---------|-----------|--------|-------------|
| 1       | 100ms     | 0-2s   | 100ms-2.1s  |
| 2       | 200ms     | 0-2s   | 200ms-2.2s  |
| 3       | 400ms     | 0-2s   | 400ms-2.4s  |
| 4       | 800ms     | 0-2s   | 800ms-2.8s  |
| 5       | 1600ms    | 0-2s   | 1.6s-3.6s   |

## Combining Patterns

Circuit Breaker and Retry work great together:

```go
// Circuit breaker protects the service
cb := gendure.NewCircuitBreaker[string](3, 30*time.Second, nil)

// Retry handles transient failures
retry := gendure.NewExponentialBackoffRetry[string](
    func() (string, error) {
        return cb.Execute(
            ctx,
            func() (string, error) {
                return callService()
            },
            func() (string, error) {
                return "", errors.New("circuit open")
            },
        )
    },
    100*time.Millisecond,
    3,
    2,
    1,
    nil,
)

result, err := retry.Execute(ctx)
```

## Context Cancellation

All operations respect `context.Context` cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// Will stop immediately if context is cancelled
result, err := retry.Execute(ctx)
if errors.Is(err, context.DeadlineExceeded) {
    fmt.Println("Operation timed out")
}
```

## Best Practices

### Circuit Breaker

- Set `failureThreshold` based on your service's error budget
- Use longer `recoveryTimeout` for services that take time to recover
- Always provide a meaningful fallback (cached data, default values, or graceful degradation)
- Monitor circuit state changes in production

### Exponential Backoff

- Start with small `initialDelay` (100-500ms) for fast operations
- Use `multiplier=2` for standard exponential growth
- Add jitter (`randomInt=1-5`) to prevent thundering herd
- Set reasonable `maxRetries` to avoid excessive delays
- Use context timeouts to bound total retry time

## Thread Safety

All components are thread-safe and can be:
- Shared across multiple goroutines
- Called concurrently
- Used in high-concurrency scenarios

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details.

## Author

Marincor

## Roadmap

Future features under consideration:

- 🚦 **Rate Limiting** - Control request rates with token bucket/leaky bucket
- 🏗️ **Bulkhead** - Isolate resources to prevent cascading failures
- ⏱️ **Timeout** - Configurable operation timeouts
- 🔄 **Fallback** - Advanced fallback strategies
- 📊 **Metrics** - Prometheus integration
- 🏥 **Health Checks** - Service health monitoring
