package gendure

import "time"

const (
	defaultFailureThreshold = 1
	defaultTimeToRecovery   = 30
	defaultRecoveryTimeout  = defaultTimeToRecovery * time.Second
)

const (
	defaultNumberToDelay = 100
	defaultInitialDelay  = defaultNumberToDelay * time.Millisecond
	defaultMaxRetries    = 3
	defaultMultiplier    = 2
	defaultRandomInt     = 1
)
