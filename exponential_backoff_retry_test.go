//nolint:all // only test
package gendure_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marincor/gendure"
	"github.com/marincor/gendure/glogger"
)

const (
	errorWantSuccessGotError = "want success, got error: %v"
	errorWantSuccessGot      = "want 'success', got '%s'"
	errorWant1CallGot        = "want 1 call, got %d"
	success                  = "success"
)

func TestExponentialBackoffRetrySuccessImmediately(t *testing.T) {
	callCount := 0
	exponetionalRetry := gendure.NewExponentialBackoffRetry(
		func() (string, error) {
			callCount++

			return success, nil
		},
		3*time.Millisecond,
		10,
		2,
		1,
		glogger.New(),
	)

	context, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	result, err := exponetionalRetry.Execute(context)
	if err != nil {
		t.Errorf(errorWantSuccessGotError, err)
	}

	if result != success {
		t.Errorf(errorWantSuccessGot, result)
	}

	if callCount != 1 {
		t.Errorf(errorWant1CallGot, callCount)
	}
}

func TestExponentialBackoffRetrySuccessAfterDelay(t *testing.T) {
	callCount := 0

	exponetionalRetry := gendure.NewExponentialBackoffRetry(
		func() (string, error) {
			callCount++
			return success, nil
		},
		3*time.Millisecond,
		10,
		2,
		1,
		glogger.New(),
	)

	time.Sleep(5 * time.Millisecond)

	result, err := exponetionalRetry.Execute(context.Background())
	if err != nil {
		t.Errorf(errorWantSuccessGotError, err)
	}

	if result != success {
		t.Errorf(errorWantSuccessGot, result)
	}

	if callCount != 1 {
		t.Errorf(errorWant1CallGot, callCount)
	}
}

func TestExponentialBackoffRetrySuccessAfterDelayAndRetriesAndJitter(t *testing.T) {
	callCount := 0

	exponetionalRetry := gendure.NewExponentialBackoffRetry(
		func() (string, error) {
			callCount++
			return success, nil
		},
		3*time.Millisecond,
		10,
		2,
		1,
		nil,
	)

	time.Sleep(7 * time.Millisecond) // Include some more time for jitter

	result, err := exponetionalRetry.Execute(context.Background())
	if err != nil {
		t.Errorf(errorWantSuccessGotError, err)
	}

	if result != success {
		t.Errorf(errorWantSuccessGot, result)
	}

	if callCount != 1 {
		t.Errorf(errorWant1CallGot, callCount)
	}
}

func TestExponentialBackoffRetrySuccessAfterRetries(t *testing.T) {
	callCount := 0

	exponetionalRetry := gendure.NewExponentialBackoffRetry(
		func() (string, error) {
			callCount++
			if callCount < 3 {
				return "", errors.ErrUnsupported
			}
			return "ok", nil
		},
		3*time.Millisecond,
		10,
		2,
		1,
		nil,
	)

	result, err := exponetionalRetry.Execute(context.Background())
	if err != nil {
		t.Errorf(errorWantSuccessGotError, err)
	}

	if result != "ok" {
		t.Errorf("want 'ok', got '%s'", result)
	}

	if callCount != 3 {
		t.Errorf("want 3 retries, got %d", callCount)
	}
}

func TestExponentialBackoffRetryFailureAfterMaxRetries(t *testing.T) {
	exponetionalRetry := gendure.NewExponentialBackoffRetry(
		func() (int, error) {
			return 0, errors.ErrUnsupported
		},
		10*time.Millisecond,
		2,
		2,
		1,
		glogger.New(),
	)

	start := time.Now()
	context, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	_, err := exponetionalRetry.Execute(context)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("want error, got nil")
	}

	if elapsed < 10*time.Millisecond {
		t.Error("want to wait between retries, got less")
	}
}

func TestGenerateJitterReturnsWithinExpectedRange(t *testing.T) {
	maxNumber := 10

	ebr := gendure.NewExponentialBackoffRetry(
		func() (int, error) {
			return 0, errors.ErrUnsupported
		},
		3*time.Millisecond,
		10,
		2,
		1,
		nil,
	)

	for range make([]int, 100) {
		j := ebr.GenerateJitter(maxNumber)
		if j < 0 || j > time.Duration(maxNumber-1)*time.Second {
			t.Errorf("jitter out of expected range: %s", j)
		}
	}
}
