package models

import (
	"time"

	"github.com/google/uuid"
)

// ── Enums ──────────────────────────────────────────────────────

type UserRole     string
type AgeGroup     string
type LanguageCode string
type IssueStatus  string
type SubmissionStatus string
type SubmissionType   string
type ContentType  string
type StoryTag     string
type ContentKind  string
type OrderFormat  string
type OrderStatus  string
type PaymentMethod string
type DeliveryZone string
type NotificationType string

const (
	RoleUser  UserRole = "user"
	RoleAdmin UserRole = "admin"

	AG5to8   AgeGroup = "5-8"
	AG9to12  AgeGroup = "9-12"
	AG13to17 AgeGroup = "13-17"

	LangEN LanguageCode = "en"
	LangSI LanguageCode = "si"
	LangTA LanguageCode = "ta"

	IssueOpen      IssueStatus = "open"
	IssueClosed    IssueStatus = "closed"
	IssuePublished IssueStatus = "published"

	StatusPending  SubmissionStatus = "pending"
	StatusLive     SubmissionStatus = "live"
	StatusRejected SubmissionStatus = "rejected"

	SubmitUpload SubmissionType = "upload"
	SubmitCamera SubmissionType = "camera"
	SubmitCanvas SubmissionType = "canvas"

	ContentEditor ContentType = "editor"
	ContentPDF    ContentType = "pdf"

	TagPoem       StoryTag = "poem"
	TagShortStory StoryTag = "short_story"
	TagArticle    StoryTag = "article"
	TagOther      StoryTag = "other"

	KindDrawing ContentKind = "drawing"
	KindArticle ContentKind = "article"

	FormatPhysical OrderFormat = "physical"
	FormatPDF      OrderFormat = "pdf"

	OrderPending  OrderStatus = "pending"
	OrderPaid     OrderStatus = "paid"
	OrderFailed   OrderStatus = "failed"
	OrderRefunded OrderStatus = "refunded"

	PayCard PaymentMethod = "card"
	PayBank PaymentMethod = "bank"
	PayCOD  PaymentMethod = "cod"

	ZoneColombo DeliveryZone = "colombo"
	ZoneOutside DeliveryZone = "outside"

	NotifTop30Selected     NotificationType = "top30_selected"
	NotifTop3Confirmed     NotificationType = "top3_confirmed"
	NotifAdminEdited       NotificationType = "admin_edited"
	NotifAdminDeleted      NotificationType = "admin_deleted"
	NotifNewCongratulation NotificationType = "new_congratulation"
	NotifNewReview         NotificationType = "new_review"
	NotifOrderConfirmed    NotificationType = "order_confirmed"

	// Prices in cents (Rs. * 100)
	PricePhysicalCents   = 35000
	PricePDFCents        = 15000
	DeliveryColomboCents = 30000
	DeliveryOutsideCents = 50000
)

// ── User ───────────────────────────────────────────────────────

type User struct {
	ID              uuid.UUID    `json:"id"`
	Email           string       `json:"email"`
	PasswordHash    string       `json:"-"`
	Role            UserRole     `json:"role"`
	Trusted         bool         `json:"trusted"`
	Active          bool         `json:"active"`
	LanguagePref    LanguageCode `json:"language_pref"`
	KidName         *string      `json:"kid_name,omitempty"`
	Age             *int16       `json:"age,omitempty"`
	AgeGroup        *AgeGroup    `json:"age_group,omitempty"`
	SchoolName      *string      `json:"school_name,omitempty"`
	GuardianName    *string      `json:"guardian_name,omitempty"`
	ContactNumber   *string      `json:"contact_number,omitempty"`
	AvatarURL       *string      `json:"avatar_url,omitempty"`
	Bio             *string      `json:"bio,omitempty"`
	ProfileComplete bool         `json:"profile_complete"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	DeactivatedAt   *time.Time   `json:"deactivated_at,omitempty"`
}

// ── Issue ──────────────────────────────────────────────────────

type Issue struct {
	ID                 uuid.UUID   `json:"id"`
	Name               string      `json:"name"`
	Description        *string     `json:"description,omitempty"`
	Deadline           time.Time   `json:"deadline"`
	PublishDate        *time.Time  `json:"publish_date,omitempty"`
	MaxDrawingsPerUser int16       `json:"max_drawings_per_user"`
	MaxArticlesPerUser int16       `json:"max_articles_per_user"`
	Status             IssueStatus `json:"status"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

// ── Drawing ────────────────────────────────────────────────────

type Drawing struct {
	ID             uuid.UUID        `json:"id"`
	UserID         uuid.UUID        `json:"user_id"`
	IssueID        uuid.UUID        `json:"issue_id"`
	Title          string           `json:"title"`
	Description    *string          `json:"description,omitempty"`
	ImageURL       string           `json:"image_url"`
	SubmissionType SubmissionType   `json:"submission_type"`
	Status         SubmissionStatus `json:"status"`
	Top30          bool             `json:"top_30"`
	AvgRating      float64          `json:"avg_rating"`
	RatingCount    int              `json:"rating_count"`
	ReviewCount    int              `json:"review_count"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	AuthorName     *string          `json:"author_name,omitempty"`
	AuthorAge      *int16           `json:"author_age,omitempty"`
	AuthorSchool   *string          `json:"author_school,omitempty"`
	IssueName      *string          `json:"issue_name,omitempty"`
}

// ── Article ────────────────────────────────────────────────────

type Article struct {
	ID          uuid.UUID        `json:"id"`
	UserID      uuid.UUID        `json:"user_id"`
	IssueID     uuid.UUID        `json:"issue_id"`
	Title       string           `json:"title"`
	Description *string          `json:"description,omitempty"`
	ContentType ContentType      `json:"content_type"`
	ContentHTML *string          `json:"content_html,omitempty"`
	PDFUrl      *string          `json:"pdf_url,omitempty"`
	Tag         StoryTag         `json:"tag"`
	Status      SubmissionStatus `json:"status"`
	Top30       bool             `json:"top_30"`
	AvgRating   float64          `json:"avg_rating"`
	RatingCount int              `json:"rating_count"`
	ReviewCount int              `json:"review_count"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	AuthorName  *string          `json:"author_name,omitempty"`
	AuthorAge   *int16           `json:"author_age,omitempty"`
	IssueName   *string          `json:"issue_name,omitempty"`
}

// ── Ratings & Reviews ──────────────────────────────────────────

type Rating struct {
	ID          uuid.UUID   `json:"id"`
	ContentKind ContentKind `json:"content_kind"`
	ContentID   uuid.UUID   `json:"content_id"`
	UserID      *uuid.UUID  `json:"user_id,omitempty"`
	Score       int16       `json:"score"`
	CreatedAt   time.Time   `json:"created_at"`
}

type Review struct {
	ID           uuid.UUID     `json:"id"`
	ContentKind  ContentKind   `json:"content_kind"`
	ContentID    uuid.UUID     `json:"content_id"`
	UserID       *uuid.UUID    `json:"user_id,omitempty"`
	GuestName    *string       `json:"guest_name,omitempty"`
	Body         string        `json:"body"`
	Flagged      bool          `json:"flagged"`
	CreatedAt    time.Time     `json:"created_at"`
	ReviewerName *string       `json:"reviewer_name,omitempty"`
	Replies      []ReviewReply `json:"replies,omitempty"`
}

type ReviewReply struct {
	ID        uuid.UUID `json:"id"`
	ReviewID  uuid.UUID `json:"review_id"`
	UserID    uuid.UUID `json:"user_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// ── Top 3 & Notifications ──────────────────────────────────────

type Top3Contributor struct {
	ID                uuid.UUID  `json:"id"`
	IssueID           uuid.UUID  `json:"issue_id"`
	UserID            uuid.UUID  `json:"user_id"`
	AgeGroup          AgeGroup   `json:"age_group"`
	Rank              int16      `json:"rank"`
	AggregateScore    float64    `json:"aggregate_score"`
	SuggestedBySystem bool       `json:"suggested_by_system"`
	ConfirmedByAdmin  bool       `json:"confirmed_by_admin"`
	ConfirmedAt       *time.Time `json:"confirmed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	ContributorName   *string    `json:"contributor_name,omitempty"`
	ContributorSchool *string    `json:"contributor_school,omitempty"`
	ContributorAvatar *string    `json:"contributor_avatar,omitempty"`
	HighlightImageURL *string    `json:"highlight_image_url,omitempty"`
}

type Congratulation struct {
	ID           uuid.UUID             `json:"id"`
	Top3ID       uuid.UUID             `json:"top3_id"`
	SenderUserID uuid.UUID             `json:"sender_user_id"`
	Body         string                `json:"body"`
	CreatedAt    time.Time             `json:"created_at"`
	SenderName   *string               `json:"sender_name,omitempty"`
	Replies      []CongratulationReply `json:"replies,omitempty"`
}

type CongratulationReply struct {
	ID               uuid.UUID `json:"id"`
	CongratulationID uuid.UUID `json:"congratulation_id"`
	UserID           uuid.UUID `json:"user_id"`
	Body             string    `json:"body"`
	CreatedAt        time.Time `json:"created_at"`
}

type Notification struct {
	ID        uuid.UUID        `json:"id"`
	UserID    uuid.UUID        `json:"user_id"`
	Type      NotificationType `json:"type"`
	MessageEN string           `json:"message_en"`
	MessageSI string           `json:"message_si"`
	MessageTA string           `json:"message_ta"`
	RelatedID *uuid.UUID       `json:"related_id,omitempty"`
	Read      bool             `json:"read"`
	CreatedAt time.Time        `json:"created_at"`
}

// ── Orders ─────────────────────────────────────────────────────

type Order struct {
	ID                   uuid.UUID     `json:"id"`
	UserID               *uuid.UUID    `json:"user_id,omitempty"`
	IssueID              uuid.UUID     `json:"issue_id"`
	BuyerName            string        `json:"buyer_name"`
	BuyerEmail           string        `json:"buyer_email"`
	BuyerPhone           *string       `json:"buyer_phone,omitempty"`
	Format               OrderFormat   `json:"format"`
	Copies               int16         `json:"copies"`
	DeliveryZone         *DeliveryZone `json:"delivery_zone,omitempty"`
	DeliveryAddressLine1 *string       `json:"delivery_address_line1,omitempty"`
	DeliveryAddressLine2 *string       `json:"delivery_address_line2,omitempty"`
	DeliveryCity         *string       `json:"delivery_city,omitempty"`
	DeliveryDistrict     *string       `json:"delivery_district,omitempty"`
	DeliveryFee          int           `json:"delivery_fee"`
	UnitPrice            int           `json:"unit_price"`
	Subtotal             int           `json:"subtotal"`
	Total                int           `json:"total"`
	PaymentMethod        PaymentMethod `json:"payment_method"`
	Status               OrderStatus   `json:"status"`
	PaidAt               *time.Time    `json:"paid_at,omitempty"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
	IssueName            *string       `json:"issue_name,omitempty"`
}

type RevenueSummary struct {
	TotalRevenueCents  int `json:"total_revenue_cents"`
	PhysicalSalesCount int `json:"physical_sales_count"`
	PDFSalesCount      int `json:"pdf_sales_count"`
	PendingCount       int `json:"pending_count"`
}

type RevenueByIssue struct {
	IssueID       uuid.UUID `json:"issue_id"`
	IssueName     string    `json:"issue_name"`
	PhysicalCents int       `json:"physical_cents"`
	PDFCents      int       `json:"pdf_cents"`
	TotalCents    int       `json:"total_cents"`
}

// ── Request / Response types ───────────────────────────────────

type RegisterRequest struct {
	Email           string `json:"email" validate:"required,email"`
	Password        string `json:"password" validate:"required,min=6"`
	Name            string `json:"name" validate:"required,max=120"`
	AgeConfirmation bool   `json:"age_confirmation"`
}
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}
type AuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         User   `json:"user"`
}
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}
type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}
type ResetPasswordRequest struct {
	Token       string `json:"token" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=6"`
}
type CompleteProfileRequest struct {
	KidName       string `json:"kid_name" validate:"required,max=120"`
	Age           int16  `json:"age" validate:"required,min=4,max=18"`
	AgeGroup      string `json:"age_group" validate:"required,oneof=5-8 9-12 13-17"`
	SchoolName    string `json:"school_name" validate:"required,max=200"`
	GuardianName  string `json:"guardian_name" validate:"required,max=120"`
	ContactNumber string `json:"contact_number" validate:"required,max=30"`
}
type UpdateProfileRequest struct {
	KidName       *string `json:"kid_name" validate:"omitempty,max=120"`
	SchoolName    *string `json:"school_name" validate:"omitempty,max=200"`
	GuardianName  *string `json:"guardian_name" validate:"omitempty,max=120"`
	ContactNumber *string `json:"contact_number" validate:"omitempty,max=30"`
	Bio           *string `json:"bio" validate:"omitempty,max=280"`
	LanguagePref  *string `json:"language_pref" validate:"omitempty,oneof=en si ta"`
}
type CreateIssueRequest struct {
	Name               string     `json:"name" validate:"required,max=100"`
	Description        string     `json:"description"`
	Deadline           time.Time  `json:"deadline" validate:"required"`
	PublishDate        *time.Time `json:"publish_date"`
	MaxDrawingsPerUser int16      `json:"max_drawings_per_user" validate:"min=1,max=20"`
	MaxArticlesPerUser int16      `json:"max_articles_per_user" validate:"min=1,max=20"`
}
type CreateDrawingRequest struct {
	Title          string `form:"title" validate:"required,max=120"`
	Description    string `form:"description" validate:"max=500"`
	IssueID        string `form:"issue_id" validate:"required,uuid"`
	SubmissionType string `form:"submission_type" validate:"required,oneof=upload camera canvas"`
}
type UpdateDrawingRequest struct {
	Title       *string `json:"title" validate:"omitempty,max=120"`
	Description *string `json:"description" validate:"omitempty,max=500"`
}
type CreateArticleRequest struct {
	Title       string `form:"title" validate:"required,max=120"`
	Description string `form:"description" validate:"max=500"`
	IssueID     string `form:"issue_id" validate:"required,uuid"`
	Tag         string `form:"tag" validate:"required,oneof=poem short_story article other"`
	ContentType string `form:"content_type" validate:"required,oneof=editor pdf"`
	ContentHTML string `form:"content_html"`
}
type UpdateArticleRequest struct {
	Title       *string `json:"title" validate:"omitempty,max=120"`
	Description *string `json:"description" validate:"omitempty,max=500"`
	ContentHTML *string `json:"content_html"`
}
type RateRequest struct {
	Score int16 `json:"score" validate:"required,min=1,max=5"`
}
type CreateReviewRequest struct {
	Body      string `json:"body" validate:"required,max=1000"`
	GuestName string `json:"guest_name" validate:"max=80"`
}
type CreateReplyRequest struct {
	Body string `json:"body" validate:"required,max=500"`
}
type CreateOrderRequest struct {
	IssueID              string `json:"issue_id" validate:"required,uuid"`
	BuyerName            string `json:"buyer_name" validate:"required,max=120"`
	BuyerEmail           string `json:"buyer_email" validate:"required,email"`
	BuyerPhone           string `json:"buyer_phone" validate:"max=30"`
	Format               string `json:"format" validate:"required,oneof=physical pdf"`
	Copies               int16  `json:"copies" validate:"min=1,max=10"`
	DeliveryZone         string `json:"delivery_zone" validate:"omitempty,oneof=colombo outside"`
	DeliveryAddressLine1 string `json:"delivery_address_line1" validate:"max=200"`
	DeliveryAddressLine2 string `json:"delivery_address_line2" validate:"max=200"`
	DeliveryCity         string `json:"delivery_city" validate:"max=100"`
	DeliveryDistrict     string `json:"delivery_district" validate:"max=100"`
	PaymentMethod        string `json:"payment_method" validate:"required,oneof=card bank cod"`
	CardNumber           string `json:"card_number,omitempty"`
	CardExpiry           string `json:"card_expiry,omitempty"`
	CardCVV              string `json:"card_cvv,omitempty"`
	CardHolder           string `json:"card_holder,omitempty"`
}
