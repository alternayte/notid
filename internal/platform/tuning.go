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
	// DeliveryBadRequestRetries is how many attempts a delivery answered with 400 gets.
	DeliveryBadRequestRetries int
	PeerInboxPerMinute        int
	PeerInboxBurst            int
	DeliveryWorkers           int
	InboxWorkers              int
	ProbeInterval             time.Duration

	PostsPerPage      int32
	TopDayWindow      time.Duration
	TopWeekWindow     time.Duration
	ThreadMaxComments int32
	SavedPerPage      int32

	ReportQueueSize int32
	// PeerListSize caps the peers an admin sees in one list.
	PeerListSize int32
	// DirectorySize caps the reviewed directory an admin sees in one list.
	DirectorySize     int32
	ModerationLogSize int32

	TrustMinAge        time.Duration
	TrustMinActivity   int
	TrustCheckInterval time.Duration
	// NewWritesPerDay caps posts and comments by a new user. A write into a
	// remote community counts against NewRemoteWritesPerDay as well.
	NewWritesPerDay       int
	NewRemoteWritesPerDay int
	InvitesPerTrustedUser int
	UploadQuotaBytes      int64
	// TransparencyWindow is the recent window the public counts report.
	TransparencyWindow time.Duration

	MaxUploadBytes    int64
	MaxImagePixels    int64
	ImageMaxDimension int
	JPEGQuality       int
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

		DeliveryRetryWindow:       72 * time.Hour,
		DeliveryMaxBackoff:        6 * time.Hour,
		DeliveryBadRequestRetries: 5,
		PeerInboxPerMinute:        300,
		PeerInboxBurst:            100,
		DeliveryWorkers:           20,
		InboxWorkers:              10,
		ProbeInterval:             time.Minute,

		PostsPerPage:      25,
		TopDayWindow:      24 * time.Hour,
		TopWeekWindow:     7 * 24 * time.Hour,
		ThreadMaxComments: 500,
		SavedPerPage:      25,

		ReportQueueSize:   100,
		PeerListSize:      200,
		DirectorySize:     200,
		ModerationLogSize: 100,

		TrustMinAge:           72 * time.Hour,
		TrustMinActivity:      5,
		TrustCheckInterval:    time.Hour,
		NewWritesPerDay:       10,
		NewRemoteWritesPerDay: 3,
		InvitesPerTrustedUser: 3,
		UploadQuotaBytes:      100 << 20,
		TransparencyWindow:    30 * 24 * time.Hour,

		MaxUploadBytes:    10 << 20,
		MaxImagePixels:    50_000_000,
		ImageMaxDimension: 2560,
		JPEGQuality:       85,
	}
}
