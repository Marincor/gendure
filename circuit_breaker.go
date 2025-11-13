package gendure

import (
	"context"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/marincor/gendure/glogger"
)

// Circuit breaker states
const (
	// Closed state allows all requests to pass through.
	// The circuit breaker monitors failures and transitions to Open when threshold is reached.
	Closed int32 = iota

	// Open state blocks all requests and immediately returns the fallback response.
	// After recoveryTimeout, transitions to HalfOpen to test if the service recovered.
	Open

	// HalfOpen state allows a single request to test service health.
	// On success, transitions back to Closed. On failure, transitions back to Open.
	HalfOpen
)

// circuitBreaker implements the Circuit Breaker resilience pattern for operations returning type T.
// It prevents cascading failures by blocking requests to failing services and providing
// automatic recovery attempts after a cooldown period.
//
// Type Parameters:
//   - T: The return type of the protected operation
//
// The circuit breaker has three states:
//   - Closed: Normal operation, requests pass through
//   - Open: Failure threshold exceeded, requests are blocked
//   - HalfOpen: Testing if service recovered, allows one request
type circuitBreaker[T any] struct {
	// lastFailureTime stores the timestamp of the most recent failure.
	// Used to determine when to transition from Open to HalfOpen state.
	lastFailureTime atomic.Value

	// typeName holds the string representation of the generic type T.
	// Used for logging and debugging purposes.
	typeName string

	// state represents the current circuit breaker state (Closed, Open, or HalfOpen).
	// Must be accessed atomically for thread-safety.
	state atomic.Int32

	// failureCount tracks the number of consecutive failures.
	// Resets to zero on successful operation or when circuit opens.
	failureCount atomic.Int32

	// failureThreshold is the maximum number of consecutive failures allowed
	// before the circuit breaker transitions to Open state.
	failureThreshold int32

	// recoveryTimeout is the duration to wait before attempting to transition
	// from Open to HalfOpen state for recovery testing.
	recoveryTimeout time.Duration

	// glogger is the optional logger instance for debugging and monitoring.
	// If nil, logging is disabled.
	glogger glogger.GLogger

	// halfOpenLock ensures only one request tests the service in HalfOpen state.
	// Prevents multiple concurrent requests from executing simultaneously during recovery testing.
	halfOpenLock atomic.Bool
}

// getTypeName extracts the string representation of a type T.
// This is used internally for logging and debugging purposes.
//
// Type Parameters:
//   - T: The type to extract the name from
//
// Parameters:
//   - t: An instance of type T (can be zero value)
//
// Returns:
//   - string: The full type name including package path
func getTypeName[T any](t T) string {
	return reflect.TypeOf(t).String()
}

// NewCircuitBreaker creates and initializes a new circuit breaker instance.
// The circuit breaker starts in Closed state, allowing all requests to pass through.
//
// Type Parameters:
//   - T: The return type of operations this circuit breaker will protect
//
// Parameters:
//   - failureThreshold: Number of consecutive failures before opening the circuit.
//     Must be greater than 0. If <= 0, defaults to 1.
//   - recoveryTimeout: Duration to wait before attempting recovery (transition to HalfOpen).
//     Must be greater than 0. If <= 0, defaults to 30 seconds.
//     Typical values range from seconds to minutes depending on the service.
//   - logger: Optional logger for debugging and monitoring. Pass nil to disable logging.
//
// Returns:
//   - *circuitBreaker[T]: A new circuit breaker instance ready for use
//
// Example:
//
//	cb := NewCircuitBreaker[string](3, 30*time.Second, myLogger)
func NewCircuitBreaker[T any](
	failureThreshold int32,
	recoveryTimeout time.Duration,
	logger glogger.GLogger,
) *circuitBreaker[T] {
	var tName T

	if failureThreshold <= 0 {
		failureThreshold = 1
	}

	if recoveryTimeout <= 0 {
		recoveryTimeout = 30 * time.Second
	}

	circuitBreaker := &circuitBreaker[T]{
		state:            atomic.Int32{},
		failureThreshold: failureThreshold,
		recoveryTimeout:  recoveryTimeout,
		typeName:         getTypeName(tName),
		glogger:          logger,
	}

	circuitBreaker.state.Store(Closed)

	return circuitBreaker
}

// Execute runs the provided operation with circuit breaker protection and context support.
// Behavior depends on circuit state:
//   - Closed: Execute operation normally
//   - Open: Skip operation and call fallback immediately (unless recovery timeout elapsed)
//   - HalfOpen: Execute operation as a test; success closes circuit, failure reopens it
//
// Context cancellation is checked before executing the operation. If the context is cancelled,
// the fallback is called immediately without executing the main operation.
//
// In HalfOpen state, only one request is allowed to test the service at a time.
// Concurrent requests during HalfOpen will use the fallback instead.
//
// This method is thread-safe and can be called concurrently.
//
// Parameters:
//   - ctx: Context for cancellation control. If cancelled, fallback is called immediately.
//   - operation: The primary function to execute. Should return (T, error).
//     Returning an error increments the failure count.
//   - fallback: Function called when circuit is Open, operation fails, or context is cancelled.
//     Provides degraded functionality or cached responses.
//
// Returns:
//   - T: Result from either operation (on success) or fallback (on failure/open circuit/cancelled context)
//   - error: Error from fallback function, or nil if operation succeeded
//
// Example:
//
//	ctx := context.Background()
//	result, err := cb.Execute(
//	    ctx,
//	    func() (string, error) { return httpClient.Get(url) },
//	    func() (string, error) { return cachedValue, nil },
//	)
func (cb *circuitBreaker[T]) Execute(
	ctx context.Context,
	operation func() (T, error),
	fallback func() (T, error),
) (T, error) {
	select {
	case <-ctx.Done():
		return fallback()
	default:
		// Check if circuit is Open
		if cb.state.Load() == Open {
			lastFailureTime, ok := cb.lastFailureTime.Load().(time.Time)
			// Transition to HalfOpen if recovery timeout has elapsed
			if ok && time.Since(lastFailureTime) > cb.recoveryTimeout {
				cb.state.Store(HalfOpen)
			} else {
				// Circuit still Open, return fallback immediately
				return fallback()
			}
		}

		if cb.state.Load() == HalfOpen {
			if !cb.halfOpenLock.CompareAndSwap(false, true) {
				return fallback()
			}

			defer cb.halfOpenLock.Store(false)
		}

		// Execute the operation
		result, err := operation()
		if err != nil {
			cb.handleFailure(ctx)
			return fallback()
		}

		// Operation succeeded, reset failure counter and ensure circuit is Closed
		cb.Reset()
		return result, nil
	}
}

// handleFailure increments the failure counter and transitions the circuit to Open state
// if the failure threshold is reached or if already in HalfOpen state.
//
// This method is called internally when an operation fails.
// Logs debug information when circuit opens (if logger is configured).
//
// Parameters:
//   - ctx: Context passed for logging purposes
func (cb *circuitBreaker[T]) handleFailure(ctx context.Context) {
	currentFailures := cb.failureCount.Add(1)

	// Open circuit if threshold reached or if testing in HalfOpen failed
	if currentFailures >= cb.failureThreshold || cb.state.Load() == HalfOpen {
		if cb.glogger != nil {
			cb.glogger.Debug(
				ctx,
				"Gendure Circuit breaker action",
				"type_name", cb.typeName,
				"failure_count", cb.failureCount.Load(),
			)
		}

		cb.state.Store(Open)
		cb.lastFailureTime.Store(time.Now())
	}
}

// GetState returns the current state of the circuit breaker.
// Thread-safe and can be called concurrently.
//
// Returns:
//   - int32: Current state (Closed, Open, or HalfOpen)
//
// Example:
//
//	if cb.GetState() == Open {
//	    log.Println("Circuit is open, requests are being blocked")
//	}
func (cb *circuitBreaker[T]) GetState() int32 {
	return cb.state.Load()
}

// GetCountFailure returns the current number of consecutive failures.
// Counter resets to zero on successful operation or when Reset() is called.
// Thread-safe and can be called concurrently.
//
// Returns:
//   - int32: Current failure count
//
// Example:
//
//	failures := cb.GetCountFailure()
//	log.Printf("Current failure count: %d", failures)
func (cb *circuitBreaker[T]) GetCountFailure() int32 {
	return cb.failureCount.Load()
}

// Reset manually resets the circuit breaker to Closed state.
// Sets failure count to zero, transitions to Closed state, and clears last failure time.
// Called automatically after successful operations.
// Thread-safe and can be called concurrently.
//
// Useful for:
//   - Administrative actions (manual recovery after fixing underlying issues)
//   - Testing scenarios
//   - Forced recovery when external monitoring indicates service is healthy
//
// Example:
//
//	// Manual reset after deployment or maintenance
//	cb.Reset()
func (cb *circuitBreaker[T]) Reset() {
	cb.failureCount.Store(0)
	cb.state.Store(Closed)
	cb.lastFailureTime.Store(time.Time{})
}
