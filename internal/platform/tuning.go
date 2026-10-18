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

	SessionIdle       time.Duration
	SessionAbsolute   time.Duration
	MagicLinkTTL      time.Duration
	APIKeyMaxTTL      time.Duration
	SignInPerEmail    int
	SignInPerIP       int
	MagicLinkPerEmail int
	MagicLinkPerIP    int
	SignUpPerIP       int

	FederationTimeout time.Duration
	MaxFetchBytes     int64
	MaxInboxBytes     int64
	SignatureMaxSkew  time.Duration
	ActorRefresh      time.Duration
	// DeliveryRetryWindow is how long a delivery retries, and how long a peer
	// may fail before deliveries to it are skipped until it answers again.
	DeliveryRetryWindow time.Duration
	DeliveryMaxBackoff  time.Duration
	PeerInboxPerMinute  int
	PeerInboxBurst      int
	DeliveryWorkers     int
	InboxWorkers        int
	ProbeInterval       time.Duration
}

func DefaultTuning() Tuning {
	return Tuning{
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ShutdownTimeout:   20 * time.Second,
		MaxHeaderBytes:    64 << 10,

		SessionIdle:       30 * 24 * time.Hour,
		SessionAbsolute:   90 * 24 * time.Hour,
		MagicLinkTTL:      15 * time.Minute,
		APIKeyMaxTTL:      365 * 24 * time.Hour,
		SignInPerEmail:    5,
		SignInPerIP:       20,
		MagicLinkPerEmail: 5,
		MagicLinkPerIP:    30,
		SignUpPerIP:       10,

		FederationTimeout: 10 * time.Second,
		MaxFetchBytes:     2 << 20,
		MaxInboxBytes:     1 << 20,
		SignatureMaxSkew:  12 * time.Hour,
		ActorRefresh:      24 * time.Hour,

		DeliveryRetryWindow: 72 * time.Hour,
		DeliveryMaxBackoff:  6 * time.Hour,
		PeerInboxPerMinute:  300,
		PeerInboxBurst:      100,
		DeliveryWorkers:     20,
		InboxWorkers:        10,
		ProbeInterval:       time.Minute,
	}
}
