package gendure_test

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/marincor/gendure"
)

var (
	errFallback             = errors.New("fallback")
	errOperation            = errors.New("operation failed")
	errOperationFailedAgain = errors.New("operation failed again")
	unexpected              = "unexpected error: %v"
)

func TestExecuteSuccess(t *testing.T) {
	t.Parallel()

	cirbuitBreaker := gendure.NewCircuitBreaker[int](3, 1*time.Second, nil)

	result, err := cirbuitBreaker.Execute(
		context.Background(),
		func() (int, error) {
			return 42, nil
		},
		func() (int, error) {
			return 0, errFallback
		},
	)
	if err != nil {
		t.Errorf(unexpected, err)
	}

	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestCircuitBreakerExecuteFailureFallBack(t *testing.T) {
	t.Parallel()

	cirbuitBreaker := gendure.NewCircuitBreaker[int](3, 1*time.Second, nil)

	// Simulate operation failure
	_, _ = cirbuitBreaker.Execute(
		context.Background(),
		func() (int, error) {
			return 0, errOperation
		},
		func() (int, error) {
			return 0, nil
		},
	)

	result, err := cirbuitBreaker.Execute(
		context.Background(),
		func() (int, error) {
			return 0, errOperationFailedAgain
		},
		func() (int, error) {
			return 99, nil
		},
	)
	if err != nil {
		t.Errorf(unexpected, err)
	}

	if result != 99 {
		t.Errorf("expected 99, got %d", result)
	}
}

func TestCircuitBreakerOpenState(t *testing.T) {
	t.Parallel()

	cirbuitBreaker := gendure.NewCircuitBreaker[int](1, 1*time.Second, nil)

	// This should open the circuit breaker
	_, _ = cirbuitBreaker.Execute(
		context.Background(),
		func() (int, error) {
			return 0, errOperation
		},
		func() (int, error) {
			return 0, nil
		},
	)

	_, err := cirbuitBreaker.Execute(
		context.Background(),
		func() (int, error) {
			t.Fatal("should not call operation when in an open state")

			return 0, nil
		},
		func() (int, error) {
			return 99, nil
		},
	)
	if err != nil {
		t.Errorf(unexpected, err)
	}
}

func TestCircuitBreakerHalfOpenState(t *testing.T) {
	t.Parallel()

	cirbuitBreaker := gendure.NewCircuitBreaker[int](3, 1*time.Second, nil)

	// Open the circuit breaker
	_, _ = cirbuitBreaker.Execute(
		context.Background(),
		func() (int, error) {
			return 0, errOperation
		},
		func() (int, error) {
			return 0, nil
		},
	)

	time.Sleep(600 * time.Millisecond) // wait for the timeout to trigger half-open state

	result, err := cirbuitBreaker.Execute(
		context.Background(),
		func() (int, error) {
			return 42, nil
		},
		func() (int, error) {
			return 0, errFallback
		},
	)
	if err != nil {
		t.Errorf(unexpected, err)
	}

	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestCircuitBreakerRaceCondition(t *testing.T) {
	failureThreshold := int32(5)
	recoveryTimeout := 100 * time.Millisecond

	cirbuitBreaker := gendure.NewCircuitBreaker[int](failureThreshold, recoveryTimeout, nil)

	numGoroutines := 100
	var wg sync.WaitGroup

	operation := func() (int, error) {
		time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
		return 0, errors.New("simulated failure")
	}

	fallback := func() (int, error) {
		time.Sleep(time.Duration(rand.Intn(40)) * time.Millisecond)
		return -1, nil
	}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cirbuitBreaker.Execute(context.Background(), operation, fallback)
		}()
	}

	wg.Wait()

	// Check the final state of the circuit breaker
	state := cirbuitBreaker.GetState()
	failureCount := cirbuitBreaker.GetCountFailure()

	if state != gendure.Open {
		t.Errorf("expected state to be Open, got %d", state)
	}

	if failureThreshold >= failureCount {
		t.Errorf("expected failure count to be less than %d, got %d", failureThreshold, failureCount)
	}
}
