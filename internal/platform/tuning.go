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
	// KeyFetchesPerMinute caps, per client address, inbox requests signed by
	// keys notid must fetch first.
	KeyFetchesPerMinute int
	KeyFetchesBurst     int
	DeliveryWorkers     int
	InboxWorkers        int
	ProbeInterval       time.Duration

	PostsPerPage      int32
	TopDayWindow      time.Duration
	TopWeekWindow     time.Duration
	ThreadMaxComments int32
	SavedPerPage      int32

	ReportQueueSize int32
	// ReportsPerDay caps the reports one person files in a day.
	ReportsPerDay int
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
	NewWritesPerDay int
	// ActorWritesPerMinute caps every state-changing API request per person.
	ActorWritesPerMinute  int
	ActorWritesBurst      int
	NewRemoteWritesPerDay int
	InvitesPerTrustedUser int
	UploadQuotaBytes      int64
	// TransparencyWindow is the recent window the public counts report.
	TransparencyWindow time.Duration

	// ProxyMaxBytes caps a remote image served through /proxy; ProxyMaxAge is
	// how long a reader's browser may cache it.
	ProxyMaxBytes int64
	ProxyMaxAge   time.Duration
	// PreviewReadBytes is how much of a linked page is read for its preview.
	PreviewReadBytes int64
	PreviewWorkers   int

	MaxUploadBytes int64
	MaxImagePixels int64
	// DecodeMaxBytes caps the memory one decode may take, from the image
	// header's size and pixel format; UploadDecoders decode at once.
	DecodeMaxBytes int64
	UploadDecoders int
	// MediaMaxAge is how long browsers keep an uploaded image.
	MediaMaxAge time.Duration
	// An upload no post or banner uses is deleted after MediaGrace, checked
	// every MediaSweepInterval.
	MediaGrace         time.Duration
	MediaSweepInterval time.Duration
	ImageMaxDimension  int
	JPEGQuality        int
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
		KeyFetchesPerMinute:       120,
		KeyFetchesBurst:           60,
		DeliveryWorkers:           20,
		InboxWorkers:              10,
		ProbeInterval:             time.Minute,

		PostsPerPage:      25,
		TopDayWindow:      24 * time.Hour,
		TopWeekWindow:     7 * 24 * time.Hour,
		ThreadMaxComments: 500,
		SavedPerPage:      25,

		ReportQueueSize:   100,
		ReportsPerDay:     20,
		PeerListSize:      200,
		DirectorySize:     200,
		ModerationLogSize: 100,

		TrustMinAge:           72 * time.Hour,
		TrustMinActivity:      5,
		TrustCheckInterval:    time.Hour,
		NewWritesPerDay:       10,
		ActorWritesPerMinute:  60,
		ActorWritesBurst:      30,
		NewRemoteWritesPerDay: 3,
		InvitesPerTrustedUser: 3,
		UploadQuotaBytes:      100 << 20,
		TransparencyWindow:    30 * 24 * time.Hour,

		ProxyMaxBytes:    10 << 20,
		ProxyMaxAge:      time.Hour,
		PreviewReadBytes: 512 << 10,
		PreviewWorkers:   4,

		MaxUploadBytes:     10 << 20,
		MaxImagePixels:     50_000_000,
		DecodeMaxBytes:     384 << 20,
		UploadDecoders:     2,
		MediaMaxAge:        24 * time.Hour,
		MediaGrace:         24 * time.Hour,
		MediaSweepInterval: time.Hour,
		ImageMaxDimension:  2560,
		JPEGQuality:        85,
	}
}
