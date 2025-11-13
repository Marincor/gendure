package gendure

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/marincor/gendure/glogger"
)

// CallbackFunc represents a function that returns a value of type T and an error.
// This is the signature for operations that will be retried using exponential backoff.
//
// Type Parameters:
//   - T: The return type of the callback function
//
// Returns:
//   - T: The result of the operation
//   - error: Error if the operation fails, nil on success
type CallbackFunc[T any] func() (T, error)

// ExponentialBackoffRetry implements the Exponential Backoff retry pattern with jitter.
// It retries failed operations with exponentially increasing delays between attempts,
// adding random jitter to prevent thundering herd problems.
//
// Type Parameters:
//   - T: The return type of the operation being retried
//
// The delay between retries follows the formula:
//
//	totalDelay = initialDelay * (multiplier ^ attempt) + randomJitter
//
// Where randomJitter is a random duration between 0 and randomInt seconds.
type ExponentialBackoffRetry[T any] struct {
	// callback is the function to be executed and retried on failure.
	callback CallbackFunc[T]

	// initialDelay is the base delay duration for the first retry attempt.
	// Subsequent delays are calculated by multiplying this value exponentially.
	initialDelay time.Duration

	// maxRetries is the maximum number of retry attempts before giving up.
	// The total number of executions will be maxRetries (including the initial attempt).
	maxRetries int

	// multiplier is the factor by which the delay increases with each attempt.
	// Typically set to 2 for exponential growth (2^attempt).
	multiplier int

	// randomInt defines the upper bound (in seconds) for random jitter.
	// A random value between 0 and randomInt-1 seconds is added to each delay.
	randomInt int

	// glogger is the optional logger instance for debugging and monitoring.
	// If nil, logging is disabled.
	glogger glogger.GLogger
}

// NewExponentialBackoffRetry creates and initializes a new exponential backoff retry instance.
// Validates input parameters and applies sensible defaults when invalid values are provided.
//
// Type Parameters:
//   - T: The return type of the operation being retried
//
// Parameters:
//   - callback: The function to execute and retry on failure. Must return (T, error).
//     Panics if nil.
//   - initialDelay: Starting delay duration before the first retry.
//     If <= 0, defaults to 100ms.
//     Common values: 100ms to 1s depending on the operation.
//   - maxRetries: Maximum number of retry attempts (including initial execution).
//     If <= 0, defaults to 3.
//     Must be greater than 0 for retries to occur.
//   - multiplier: Exponential growth factor for delays. Typically 2 for doubling.
//     If <= 0, defaults to 2.
//     Higher values result in more aggressive backoff.
//   - randomInt: Maximum jitter in seconds (0 to randomInt-1).
//     If <= 0, defaults to 1.
//     Helps distribute retry attempts and prevent thundering herd.
//     Common values: 1-5 seconds.
//   - glogger: Optional logger for debugging. Pass nil to disable logging.
//
// Returns:
//   - ExponentialBackoffRetry[T]: A configured retry instance ready for use
//
// Panics:
//   - If callback is nil
//
// Example:
//
//	retry := NewExponentialBackoffRetry[string](
//	    func() (string, error) { return httpClient.Get(url) },
//	    100*time.Millisecond, // initial delay
//	    5,                     // max retries
//	    2,                     // multiplier (exponential)
//	    3,                     // jitter up to 3 seconds
//	    myLogger,
//	)
func NewExponentialBackoffRetry[T any](
	callback CallbackFunc[T],
	initialDelay time.Duration,
	maxRetries, multiplier, randomInt int,
	glogger glogger.GLogger,
) ExponentialBackoffRetry[T] {
	if callback == nil {
		panic("callback cannot be nil")
	}

	if initialDelay <= 0 {
		initialDelay = 100 * time.Millisecond
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if multiplier <= 0 {
		multiplier = 2
	}
	if randomInt <= 0 {
		randomInt = 1
	}

	return ExponentialBackoffRetry[T]{
		callback:     callback,
		initialDelay: initialDelay,
		maxRetries:   maxRetries,
		multiplier:   multiplier,
		randomInt:    randomInt,
		glogger:      glogger,
	}
}

// Execute runs the callback function with exponential backoff retry logic and context cancellation support.
// The operation is retried up to maxRetries times with exponentially increasing delays.
// Respects context cancellation both before callback execution and during delays.
//
// Execution flow:
//  1. Checks if context is cancelled before each attempt
//  2. Attempts to execute the callback
//  3. If successful, returns the result immediately
//  4. If failed and retries remain, waits for (exponential delay + jitter)
//  5. During the delay, monitors context cancellation for early termination
//  6. Repeats until success, maxRetries exhausted, or context cancelled
//
// The delay calculation uses bit shifting for efficient exponential growth:
// delay = initialDelay * (multiplier^attempt), where multiplier<<attempt equals 2^attempt when multiplier=2
//
// Parameters:
//   - ctx: Context for cancellation control. If cancelled at any point (before execution
//     or during delay), the function returns immediately with ctx.Err().
//
// Returns:
//   - T: The result from the callback if any attempt succeeds, or zero value if context cancelled
//   - error: nil if successful, ctx.Err() if context cancelled, or the last callback error if retries exhausted
//
// Thread-safety:
//   - Safe to call concurrently from multiple goroutines
//   - Each invocation maintains its own retry state
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	result, err := retry.Execute(ctx)
//	if err != nil {
//	    if errors.Is(err, context.DeadlineExceeded) {
//	        log.Println("Retry timed out")
//	    } else {
//	        log.Printf("All retry attempts failed: %v", err)
//	    }
//	}
func (ebr ExponentialBackoffRetry[T]) Execute(ctx context.Context) (T, error) {
	var attempt int

	for {
		// Check if context is cancelled before attempting operation
		select {
		case <-ctx.Done():
			var zero T

			return zero, ctx.Err()
		default:
		}

		result, err := ebr.callback()
		if err == nil {
			return result, nil
		}

		// Check if we've exhausted all retry attempts
		if attempt >= ebr.maxRetries-1 {
			var zero T

			return zero, err
		}

		delay := ebr.initialDelay * time.Duration(ebr.multiplier<<attempt) // 2^attempt

		jitter := ebr.GenerateJitter(ebr.randomInt)

		totalDelay := delay + jitter

		if ebr.glogger != nil {
			ebr.glogger.Debug(
				ctx,
				"Gendure Exponential Backoff Retry",
				"attempt", attempt,
				"delay", delay,
				"jitter", jitter,
				"total_delay", totalDelay,
			)
		}

		// Wait for delay with context cancellation support
		timer := time.NewTimer(totalDelay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			var zero T

			return zero, ctx.Err()
		case <-timer.C:
			// Delay completed, proceed to next attempt
		}

		attempt++
	}
}

// GenerateJitter generates a random duration to add to retry delays.
// This prevents the "thundering herd" problem where multiple clients
// retry simultaneously, overwhelming the recovering service.
//
// The jitter is calculated using cryptographically secure random bytes
// to ensure good distribution of retry attempts across time.
//
// Parameters:
//   - randomInt: Maximum jitter value in seconds. The actual jitter will be
//     between 0 and (randomInt-1) seconds.
//
// Returns:
//   - time.Duration: Random jitter duration between 0 and (randomInt-1) seconds
//
// Implementation notes:
//   - Uses crypto/rand for secure random number generation
//   - Falls back to randomInt if random generation fails
//   - Uses modulo operation to constrain the random value to the desired range
//
// Example:
//
//	jitter := ebr.GenerateJitter(5) // Returns 0-4 seconds randomly
func (ebr ExponentialBackoffRetry[T]) GenerateJitter(randomInt int) time.Duration {
	necessaryAmountOfBytes := 1
	randomValue := make([]byte, necessaryAmountOfBytes)
	randomByte := randomInt

	if _, err := rand.Read(randomValue); err == nil {
		randomByte = int(randomValue[0])
	}

	// limit bytes between 0 and randomInt -1 (because of % operator)
	jitter := time.Duration(randomByte%randomInt) * time.Second

	return jitter
}
