package platform

import "time"

// Tuning holds every operational limit. Changing a value is a code change with a test.
type Tuning struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

func DefaultTuning() Tuning {
	return Tuning{
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ShutdownTimeout:   20 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}
