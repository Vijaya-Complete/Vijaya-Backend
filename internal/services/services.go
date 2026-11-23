// Package services contains all business logic for the Vijaya backend.
// All services are thin wrappers over pgxpool.Pool — handlers stay
// HTTP-only and delegate every database and domain decision here.
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/resend/resend-go/v2"

	"vijaya-backend/internal/auth"
	"vijaya-backend/internal/config"
	"vijaya-backend/internal/models"
)

// ── Sentinel errors ────────────────────────────────────────────

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountDeactivated = errors.New("account deactivated")
	ErrUserNotFound       = errors.New("user not found")
	ErrDrawingNotFound    = errors.New("drawing not found")
	ErrArticleNotFound    = errors.New("article not found")
	ErrIssueNotFound      = errors.New("issue not found")
	ErrOrderNotFound      = errors.New("order not found")
	ErrNotOwner           = errors.New("you do not own this")
	ErrSubmissionLimit    = errors.New("submission limit reached for this issue")
)

// ── Helpers ────────────────────────────────────────────────────

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ══════════════════════════════════════════════════════════════
// AuthService
// ══════════════════════════════════════════════════════════════

type AuthService struct {
	db  *pgxpool.Pool
	rdb *redis.Client
	tm  *auth.TokenManager
}

func NewAuthService(db *pgxpool.Pool, rdb *redis.Client, tm *auth.TokenManager) *AuthService {
	return &AuthService{db: db, rdb: rdb, tm: tm}
}

func refreshKey(userID uuid.UUID, jti string) string {
	return fmt.Sprintf("refresh:%s:%s", userID, jti)
}

func (s *AuthService) Register(ctx context.Context, req models.RegisterRequest) (*models.User, error) {
	var exists bool
	s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, req.Email).Scan(&exists)
	if exists {
		return nil, ErrEmailTaken
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	var u models.User
	err = s.db.QueryRow(ctx, `
		INSERT INTO users(email,password_hash,role,profile_complete)
		VALUES($1,$2,'user',FALSE)
		RETURNING id,email,role,trusted,active,language_pref,profile_complete,created_at,updated_at`,
		req.Email, hash,
	).Scan(&u.ID, &u.Email, &u.Role, &u.Trusted, &u.Active, &u.LanguagePref,
		&u.ProfileComplete, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}
	return &u, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (*models.User, string, string, error) {
	var u models.User
	var hash string
	err := s.db.QueryRow(ctx, `
		SELECT id,email,password_hash,role,trusted,active,language_pref,profile_complete,created_at,updated_at
		FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.Email, &hash, &u.Role, &u.Trusted, &u.Active,
		&u.LanguagePref, &u.ProfileComplete, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", "", ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", "", err
	}
	if !auth.CheckPassword(hash, password) {
		return nil, "", "", ErrInvalidCredentials
	}
	if !u.Active {
		return nil, "", "", ErrAccountDeactivated
	}
	access, err := s.tm.GenerateAccessToken(u.ID, u.Email, string(u.Role))
	if err != nil {
		return nil, "", "", err
	}
	refresh, jti, err := s.tm.GenerateRefreshToken(u.ID)
	if err != nil {
		return nil, "", "", err
	}
	s.rdb.Set(ctx, refreshKey(u.ID, jti), "valid", s.tm.RefreshTTL())
	return &u, access, refresh, nil
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (string, string, error) {
	claims, err := s.tm.ValidateRefreshToken(refreshToken)
	if err != nil {
		return "", "", auth.ErrInvalidToken
	}
	userID, _ := uuid.Parse(claims.Subject)
	key := refreshKey(userID, claims.ID)
	n, err := s.rdb.Exists(ctx, key).Result()
	if err != nil || n == 0 {
		return "", "", auth.ErrInvalidToken
	}
	s.rdb.Del(ctx, key)
	var email, role string
	if err := s.db.QueryRow(ctx, `SELECT email,role FROM users WHERE id=$1 AND active=TRUE`, userID).
		Scan(&email, &role); err != nil {
		return "", "", ErrUserNotFound
	}
	newAccess, _ := s.tm.GenerateAccessToken(userID, email, role)
	newRefresh, newJTI, _ := s.tm.GenerateRefreshToken(userID)
	s.rdb.Set(ctx, refreshKey(userID, newJTI), "valid", s.tm.RefreshTTL())
	return newAccess, newRefresh, nil
}

func (s *AuthService) Logout(ctx context.Context, refreshToken string) {
	claims, err := s.tm.ValidateRefreshToken(refreshToken)
	if err != nil {
		return
	}
	userID, _ := uuid.Parse(claims.Subject)
	s.rdb.Del(ctx, refreshKey(userID, claims.ID))
}

func (s *AuthService) CompleteProfile(ctx context.Context, userID uuid.UUID, req models.CompleteProfileRequest) (*models.User, error) {
	var u models.User
	err := s.db.QueryRow(ctx, `
		UPDATE users SET kid_name=$1,age=$2,age_group=$3,school_name=$4,
		                 guardian_name=$5,contact_number=$6,profile_complete=TRUE,updated_at=now()
		WHERE id=$7
		RETURNING id,email,role,trusted,active,language_pref,kid_name,age,age_group,
		          school_name,guardian_name,contact_number,avatar_url,bio,profile_complete,created_at,updated_at`,
		req.KidName, req.Age, req.AgeGroup, req.SchoolName, req.GuardianName, req.ContactNumber, userID,
	).Scan(&u.ID, &u.Email, &u.Role, &u.Trusted, &u.Active, &u.LanguagePref,
		&u.KidName, &u.Age, &u.AgeGroup, &u.SchoolName, &u.GuardianName, &u.ContactNumber,
		&u.AvatarURL, &u.Bio, &u.ProfileComplete, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return &u, err
}

func (s *AuthService) ForgotPassword(ctx context.Context, email string) (string, *models.User, error) {
	var u models.User
	err := s.db.QueryRow(ctx, `SELECT id,email FROM users WHERE email=$1 AND active=TRUE`, email).
		Scan(&u.ID, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	token := uuid.NewString()
	s.rdb.Set(ctx, "pwreset:"+token, u.ID.String(), time.Hour)
	return token, &u, nil
}

func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword string) error {
	idStr, err := s.rdb.Get(ctx, "pwreset:"+token).Result()
	if errors.Is(err, redis.Nil) {
		return auth.ErrInvalidToken
	}
	if err != nil {
		return err
	}
	userID, _ := uuid.Parse(idStr)
	hash, _ := auth.HashPassword(newPassword)
	_, err = s.db.Exec(ctx, `UPDATE users SET password_hash=$1,updated_at=now() WHERE id=$2`, hash, userID)
	s.rdb.Del(ctx, "pwreset:"+token)
	return err
}

// ══════════════════════════════════════════════════════════════
// UserService
// ══════════════════════════════════════════════════════════════

type UserService struct{ db *pgxpool.Pool }

func NewUserService(db *pgxpool.Pool) *UserService { return &UserService{db: db} }

func (s *UserService) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var u models.User
	err := s.db.QueryRow(ctx, `
		SELECT id,email,role,trusted,active,language_pref,kid_name,age,age_group,
		       school_name,guardian_name,contact_number,avatar_url,bio,profile_complete,created_at,updated_at
		FROM users WHERE id=$1`, id,
	).Scan(&u.ID, &u.Email, &u.Role, &u.Trusted, &u.Active, &u.LanguagePref,
		&u.KidName, &u.Age, &u.AgeGroup, &u.SchoolName, &u.GuardianName, &u.ContactNumber,
		&u.AvatarURL, &u.Bio, &u.ProfileComplete, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return &u, err
}

func (s *UserService) UpdateProfile(ctx context.Context, id uuid.UUID, req models.UpdateProfileRequest) (*models.User, error) {
	_, err := s.db.Exec(ctx, `
		UPDATE users SET
			kid_name=COALESCE($1,kid_name), school_name=COALESCE($2,school_name),
			guardian_name=COALESCE($3,guardian_name), contact_number=COALESCE($4,contact_number),
			bio=COALESCE($5,bio), language_pref=COALESCE($6,language_pref), updated_at=now()
		WHERE id=$7`,
		req.KidName, req.SchoolName, req.GuardianName, req.ContactNumber,
		req.Bio, req.LanguagePref, id)
	if err != nil {
		return nil, err
	}
	return s.GetByID(ctx, id)
}

func (s *UserService) UpdateAvatar(ctx context.Context, id uuid.UUID, url string) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET avatar_url=$1,updated_at=now() WHERE id=$2`, url, id)
	return err
}

func (s *UserService) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}

type SubmissionsSummary struct {
	DrawingsTotal int `json:"drawings_total"`
	DrawingsTop30 int `json:"drawings_top30"`
	ArticlesTotal int `json:"articles_total"`
	ArticlesTop30 int `json:"articles_top30"`
}

func (s *UserService) GetSubmissionsSummary(ctx context.Context, id uuid.UUID) (*SubmissionsSummary, error) {
	var sum SubmissionsSummary
	err := s.db.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM drawings WHERE user_id=$1),
			(SELECT COUNT(*) FROM drawings WHERE user_id=$1 AND top_30=TRUE),
			(SELECT COUNT(*) FROM articles WHERE user_id=$1),
			(SELECT COUNT(*) FROM articles WHERE user_id=$1 AND top_30=TRUE)`, id,
	).Scan(&sum.DrawingsTotal, &sum.DrawingsTop30, &sum.ArticlesTotal, &sum.ArticlesTop30)
	return &sum, err
}

// ══════════════════════════════════════════════════════════════
// NotificationService
// ══════════════════════════════════════════════════════════════

type NotificationService struct{ db *pgxpool.Pool }

func NewNotificationService(db *pgxpool.Pool) *NotificationService {
	return &NotificationService{db: db}
}

func (s *NotificationService) create(ctx context.Context, userID uuid.UUID,
	t models.NotificationType, en, si, ta string, relatedID *uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO notifications(user_id,type,message_en,message_si,message_ta,related_id) VALUES($1,$2,$3,$4,$5,$6)`,
		userID, t, en, si, ta, relatedID)
	return err
}

func (s *NotificationService) NotifyTop30Selected(ctx context.Context, userID uuid.UUID, kind, title, issueName string, id uuid.UUID) error {
	return s.create(ctx, userID, models.NotifTop30Selected,
		fmt.Sprintf("Your %s '%s' was selected for Top 30 in %s! 🌟", kind, title, issueName),
		fmt.Sprintf("ඔබේ %s '%s' %s හොඳම 30ට ‍ ‍ ‍ ‍ 🌟", kind, title, issueName),
		fmt.Sprintf("உங்கள் %s '%s' சிறந்த 30ல்! 🌟", kind, title, issueName),
		&id)
}

func (s *NotificationService) NotifyTop3Confirmed(ctx context.Context, userID uuid.UUID, issueName string, id uuid.UUID) error {
	return s.create(ctx, userID, models.NotifTop3Confirmed,
		fmt.Sprintf("You've been named a Top 3 Contributor for %s! 🏆", issueName),
		fmt.Sprintf("ඔබ %s හොඳම 3 දායකයෙකු! 🏆", issueName),
		fmt.Sprintf("நீங்கள் %s சிறந்த 3 பங்களிப்பாளர்! 🏆", issueName),
		&id)
}

func (s *NotificationService) NotifyAdminEdited(ctx context.Context, userID uuid.UUID, kind, title string, id uuid.UUID) error {
	return s.create(ctx, userID, models.NotifAdminEdited,
		fmt.Sprintf("Admin edited your %s '%s'.", kind, title),
		fmt.Sprintf("පරිපාලකයා ඔබේ %s '%s' සංස්කරණය කළේය.", kind, title),
		fmt.Sprintf("நிர்வாகி உங்கள் %s '%s' திருத்தினார்.", kind, title),
		&id)
}

func (s *NotificationService) NotifyAdminDeleted(ctx context.Context, userID uuid.UUID, kind, title string) error {
	return s.create(ctx, userID, models.NotifAdminDeleted,
		fmt.Sprintf("Admin removed your %s '%s'.", kind, title),
		fmt.Sprintf("පරිපාලකයා ඔබේ %s '%s' ඉවත් කළේය.", kind, title),
		fmt.Sprintf("நிர்வாகி உங்கள் %s '%s' நீக்கினார்.", kind, title),
		nil)
}

func (s *NotificationService) NotifyNewReview(ctx context.Context, userID uuid.UUID, kind, title string, id uuid.UUID) error {
	return s.create(ctx, userID, models.NotifNewReview,
		fmt.Sprintf("New review on your %s '%s'.", kind, title),
		fmt.Sprintf("ඔබේ %s '%s' සඳහා නව සමාලෝචනයක්.", kind, title),
		fmt.Sprintf("உங்கள் %s '%s' இல் புதிய மதிப்பாய்வு.", kind, title),
		&id)
}

func (s *NotificationService) NotifyOrderConfirmed(ctx context.Context, userID uuid.UUID, issueName string, id uuid.UUID) error {
	return s.create(ctx, userID, models.NotifOrderConfirmed,
		fmt.Sprintf("Your order for %s has been confirmed!", issueName),
		fmt.Sprintf("%s ඇණවුම තහවුරු!", issueName),
		fmt.Sprintf("%s ஆர்டர் உறுதி!", issueName),
		&id)
}

func (s *NotificationService) ListForUser(ctx context.Context, userID uuid.UUID) ([]models.Notification, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id,user_id,type,message_en,message_si,message_ta,related_id,read,created_at
		 FROM notifications WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Notification
	for rows.Next() {
		var n models.Notification
		rows.Scan(&n.ID, &n.UserID, &n.Type, &n.MessageEN, &n.MessageSI, &n.MessageTA,
			&n.RelatedID, &n.Read, &n.CreatedAt)
		out = append(out, n)
	}
	return out, nil
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE notifications SET read=TRUE WHERE user_id=$1 AND read=FALSE`, userID)
	return err
}

// ══════════════════════════════════════════════════════════════
// EmailService
// ══════════════════════════════════════════════════════════════

type EmailService struct {
	client      *resend.Client
	from        string
	frontendURL string
}

func NewEmailService(cfg *config.Config) *EmailService {
	return &EmailService{
		client:      resend.NewClient(cfg.ResendAPIKey),
		from:        cfg.EmailFrom,
		frontendURL: cfg.FrontendURL,
	}
}

func (s *EmailService) send(to, subject, html string) {
	if s.client == nil {
		log.Printf("[email] skipped (no API key) — to=%s", to)
		return
	}
	_, err := s.client.Emails.Send(&resend.SendEmailRequest{
		From: s.from, To: []string{to}, Subject: subject, Html: html,
	})
	if err != nil {
		log.Printf("[email] failed to %s: %v", to, err)
	}
}

func (s *EmailService) SendPasswordReset(to, token string) {
	link := fmt.Sprintf("%s/reset-password?token=%s", s.frontendURL, token)
	s.send(to, "Reset your Vijaya password",
		fmt.Sprintf(`<h2>Reset your password</h2><p><a href="%s">Reset my password</a> — expires in 1 hour.</p>`, link))
}

func (s *EmailService) SendOrderConfirmation(to, buyerName, issueName, format string, totalRs int) {
	s.send(to, "Your Vijaya Magazine order", fmt.Sprintf(
		`<h2>Thank you, %s!</h2><p>Order for <b>%s</b> (%s). Total: <b>Rs. %d</b>.</p>`,
		buyerName, issueName, format, totalRs))
}

func (s *EmailService) SendContactReply(to, original, reply string) {
	s.send(to, "Reply from Vijaya Magazine", fmt.Sprintf(
		`<p>%s</p><hr/><small>Your message: "%s"</small>`, reply, original))
}

// ══════════════════════════════════════════════════════════════
// IssueService
// ══════════════════════════════════════════════════════════════

type IssueService struct{ db *pgxpool.Pool }

func NewIssueService(db *pgxpool.Pool) *IssueService { return &IssueService{db: db} }

const issueCols = `id,name,description,deadline,publish_date,max_drawings_per_user,max_articles_per_user,status,created_at,updated_at`

func scanIssue(row interface{ Scan(...interface{}) error }, i *models.Issue) error {
	return row.Scan(&i.ID, &i.Name, &i.Description, &i.Deadline, &i.PublishDate,
		&i.MaxDrawingsPerUser, &i.MaxArticlesPerUser, &i.Status, &i.CreatedAt, &i.UpdatedAt)
}

func (s *IssueService) List(ctx context.Context) ([]models.Issue, error) {
	rows, err := s.db.Query(ctx, `SELECT `+issueCols+` FROM issues ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Issue
	for rows.Next() {
		var i models.Issue
		scanIssue(rows, &i)
		out = append(out, i)
	}
	return out, nil
}

func (s *IssueService) GetActive(ctx context.Context) (*models.Issue, error) {
	var i models.Issue
	err := scanIssue(s.db.QueryRow(ctx,
		`SELECT `+issueCols+` FROM issues WHERE status='open' ORDER BY deadline ASC LIMIT 1`), &i)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIssueNotFound
	}
	return &i, err
}

func (s *IssueService) GetByID(ctx context.Context, id uuid.UUID) (*models.Issue, error) {
	var i models.Issue
	err := scanIssue(s.db.QueryRow(ctx,
		`SELECT `+issueCols+` FROM issues WHERE id=$1`, id), &i)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIssueNotFound
	}
	return &i, err
}

func (s *IssueService) Create(ctx context.Context, req models.CreateIssueRequest) (*models.Issue, error) {
	var i models.Issue
	err := scanIssue(s.db.QueryRow(ctx, `
		INSERT INTO issues(name,description,deadline,publish_date,max_drawings_per_user,max_articles_per_user)
		VALUES($1,$2,$3,$4,$5,$6) RETURNING `+issueCols,
		req.Name, nullStr(req.Description), req.Deadline, req.PublishDate,
		req.MaxDrawingsPerUser, req.MaxArticlesPerUser), &i)
	return &i, err
}

func (s *IssueService) Update(ctx context.Context, id uuid.UUID, req models.CreateIssueRequest) (*models.Issue, error) {
	var i models.Issue
	err := scanIssue(s.db.QueryRow(ctx, `
		UPDATE issues SET name=$1,description=$2,deadline=$3,publish_date=$4,
		                  max_drawings_per_user=$5,max_articles_per_user=$6,updated_at=now()
		WHERE id=$7 RETURNING `+issueCols,
		req.Name, nullStr(req.Description), req.Deadline, req.PublishDate,
		req.MaxDrawingsPerUser, req.MaxArticlesPerUser, id), &i)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIssueNotFound
	}
	return &i, err
}

func (s *IssueService) SetStatus(ctx context.Context, id uuid.UUID, status models.IssueStatus) error {
	_, err := s.db.Exec(ctx, `UPDATE issues SET status=$1,updated_at=now() WHERE id=$2`, status, id)
	return err
}

// ══════════════════════════════════════════════════════════════
// DrawingService
// ══════════════════════════════════════════════════════════════

type DrawingListParams struct {
	IssueID  *uuid.UUID
	Status   *models.SubmissionStatus
	AgeGroup *models.AgeGroup
	Top30    *bool
	SortBy   string
	Page     int
	PageSize int
}

type DrawingService struct{ db *pgxpool.Pool }

func NewDrawingService(db *pgxpool.Pool) *DrawingService { return &DrawingService{db: db} }

const dCols = `d.id,d.user_id,d.issue_id,d.title,d.description,d.image_url,d.submission_type,d.status,d.top_30,d.avg_rating,d.rating_count,d.review_count,d.created_at,d.updated_at,u.kid_name,u.age,u.school_name,i.name`
const dJoin = `FROM drawings d JOIN users u ON u.id=d.user_id JOIN issues i ON i.id=d.issue_id`

func scanDrawing(row interface{ Scan(...interface{}) error }, d *models.Drawing) error {
	return row.Scan(&d.ID, &d.UserID, &d.IssueID, &d.Title, &d.Description, &d.ImageURL,
		&d.SubmissionType, &d.Status, &d.Top30, &d.AvgRating, &d.RatingCount, &d.ReviewCount,
		&d.CreatedAt, &d.UpdatedAt, &d.AuthorName, &d.AuthorAge, &d.AuthorSchool, &d.IssueName)
}

func (s *DrawingService) GetByID(ctx context.Context, id uuid.UUID) (*models.Drawing, error) {
	var d models.Drawing
	err := scanDrawing(s.db.QueryRow(ctx, `SELECT `+dCols+` `+dJoin+` WHERE d.id=$1`, id), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDrawingNotFound
	}
	return &d, err
}

func (s *DrawingService) Create(ctx context.Context, userID, issueID uuid.UUID, req models.CreateDrawingRequest, imageURL string, trusted bool) (*models.Drawing, error) {
	var count, max int
	s.db.QueryRow(ctx, `SELECT COUNT(*),(SELECT max_drawings_per_user FROM issues WHERE id=$2) FROM drawings WHERE user_id=$1 AND issue_id=$2`, userID, issueID).Scan(&count, &max)
	if count >= max {
		return nil, ErrSubmissionLimit
	}
	status := models.StatusPending
	if trusted {
		status = models.StatusLive
	}
	var d models.Drawing
	err := s.db.QueryRow(ctx, `
		INSERT INTO drawings(user_id,issue_id,title,description,image_url,submission_type,status)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id,user_id,issue_id,title,description,image_url,submission_type,status,top_30,avg_rating,rating_count,review_count,created_at,updated_at`,
		userID, issueID, req.Title, nullStr(req.Description), imageURL, req.SubmissionType, status,
	).Scan(&d.ID, &d.UserID, &d.IssueID, &d.Title, &d.Description, &d.ImageURL,
		&d.SubmissionType, &d.Status, &d.Top30, &d.AvgRating, &d.RatingCount, &d.ReviewCount,
		&d.CreatedAt, &d.UpdatedAt)
	return &d, err
}

func (s *DrawingService) List(ctx context.Context, p DrawingListParams) ([]models.Drawing, int, error) {
	if p.PageSize == 0 { p.PageSize = 20 }
	if p.Page == 0 { p.Page = 1 }
	order := "d.created_at DESC"
	if p.SortBy == "rating" { order = "d.avg_rating DESC" }
	where, args, n := "WHERE 1=1", []interface{}{}, 1
	if p.IssueID != nil { where += fmt.Sprintf(" AND d.issue_id=$%d", n); args = append(args, *p.IssueID); n++ }
	if p.Status != nil { where += fmt.Sprintf(" AND d.status=$%d", n); args = append(args, *p.Status); n++ } else { where += fmt.Sprintf(" AND d.status=$%d", n); args = append(args, models.StatusLive); n++ }
	if p.AgeGroup != nil { where += fmt.Sprintf(" AND u.age_group=$%d", n); args = append(args, *p.AgeGroup); n++ }
	if p.Top30 != nil { where += fmt.Sprintf(" AND d.top_30=$%d", n); args = append(args, *p.Top30); n++ }
	var total int
	ca := make([]interface{}, len(args)); copy(ca, args)
	s.db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM drawings d JOIN users u ON u.id=d.user_id %s`, where), ca...).Scan(&total)
	args = append(args, p.PageSize, (p.Page-1)*p.PageSize)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s %s %s ORDER BY %s LIMIT $%d OFFSET $%d`, dCols, dJoin, where, order, n, n+1), args...)
	if err != nil { return nil, 0, err }
	defer rows.Close()
	var list []models.Drawing
	for rows.Next() { var d models.Drawing; scanDrawing(rows, &d); list = append(list, d) }
	return list, total, nil
}

func (s *DrawingService) ListByUser(ctx context.Context, userID uuid.UUID) ([]models.Drawing, error) {
	rows, err := s.db.Query(ctx, `SELECT `+dCols+` `+dJoin+` WHERE d.user_id=$1 ORDER BY d.created_at DESC`, userID)
	if err != nil { return nil, err }
	defer rows.Close()
	var list []models.Drawing
	for rows.Next() { var d models.Drawing; scanDrawing(rows, &d); list = append(list, d) }
	return list, nil
}

func (s *DrawingService) Update(ctx context.Context, id, callerID uuid.UUID, req models.UpdateDrawingRequest, isAdmin bool) (*models.Drawing, error) {
	d, err := s.GetByID(ctx, id)
	if err != nil { return nil, err }
	if !isAdmin && d.UserID != callerID { return nil, ErrNotOwner }
	if req.Title != nil { d.Title = *req.Title }
	if req.Description != nil { d.Description = req.Description }
	s.db.Exec(ctx, `UPDATE drawings SET title=$1,description=$2,updated_at=now() WHERE id=$3`, d.Title, d.Description, id)
	return s.GetByID(ctx, id)
}

func (s *DrawingService) UpdateImage(ctx context.Context, id, callerID uuid.UUID, url string, isAdmin bool) error {
	d, err := s.GetByID(ctx, id)
	if err != nil { return err }
	if !isAdmin && d.UserID != callerID { return ErrNotOwner }
	_, err = s.db.Exec(ctx, `UPDATE drawings SET image_url=$1,updated_at=now() WHERE id=$2`, url, id)
	return err
}

func (s *DrawingService) Delete(ctx context.Context, id, callerID uuid.UUID, isAdmin bool) (string, error) {
	d, err := s.GetByID(ctx, id)
	if err != nil { return "", err }
	if !isAdmin && d.UserID != callerID { return "", ErrNotOwner }
	s.db.Exec(ctx, `DELETE FROM drawings WHERE id=$1`, id)
	return d.ImageURL, nil
}

func (s *DrawingService) UpdateRatingStats(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE drawings SET avg_rating=COALESCE((SELECT AVG(score) FROM ratings WHERE content_kind='drawing' AND content_id=$1),0),rating_count=(SELECT COUNT(*) FROM ratings WHERE content_kind='drawing' AND content_id=$1),updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *DrawingService) UpdateReviewCount(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE drawings SET review_count=(SELECT COUNT(*) FROM reviews WHERE content_kind='drawing' AND content_id=$1),updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *DrawingService) SetTop30(ctx context.Context, id uuid.UUID, val bool) error {
	_, err := s.db.Exec(ctx, `UPDATE drawings SET top_30=$1,updated_at=now() WHERE id=$2`, val, id)
	return err
}

func (s *DrawingService) SetStatus(ctx context.Context, id uuid.UUID, st models.SubmissionStatus) error {
	_, err := s.db.Exec(ctx, `UPDATE drawings SET status=$1,updated_at=now() WHERE id=$2`, st, id)
	return err
}

// ══════════════════════════════════════════════════════════════
// ArticleService
// ══════════════════════════════════════════════════════════════

type ArticleListParams struct {
	IssueID  *uuid.UUID
	Status   *models.SubmissionStatus
	Tag      *models.StoryTag
	Top30    *bool
	SortBy   string
	Page     int
	PageSize int
}

type ArticleService struct{ db *pgxpool.Pool }

func NewArticleService(db *pgxpool.Pool) *ArticleService { return &ArticleService{db: db} }

const aCols = `a.id,a.user_id,a.issue_id,a.title,a.description,a.content_type,a.content_html,a.pdf_url,a.tag,a.status,a.top_30,a.avg_rating,a.rating_count,a.review_count,a.created_at,a.updated_at,u.kid_name,u.age,i.name`
const aJoin = `FROM articles a JOIN users u ON u.id=a.user_id JOIN issues i ON i.id=a.issue_id`

func scanArticle(row interface{ Scan(...interface{}) error }, a *models.Article) error {
	return row.Scan(&a.ID, &a.UserID, &a.IssueID, &a.Title, &a.Description, &a.ContentType,
		&a.ContentHTML, &a.PDFUrl, &a.Tag, &a.Status, &a.Top30, &a.AvgRating, &a.RatingCount,
		&a.ReviewCount, &a.CreatedAt, &a.UpdatedAt, &a.AuthorName, &a.AuthorAge, &a.IssueName)
}

func (s *ArticleService) GetByID(ctx context.Context, id uuid.UUID) (*models.Article, error) {
	var a models.Article
	err := scanArticle(s.db.QueryRow(ctx, `SELECT `+aCols+` `+aJoin+` WHERE a.id=$1`, id), &a)
	if errors.Is(err, pgx.ErrNoRows) { return nil, ErrArticleNotFound }
	return &a, err
}

func (s *ArticleService) Create(ctx context.Context, userID, issueID uuid.UUID, req models.CreateArticleRequest, pdfURL string, trusted bool) (*models.Article, error) {
	var count, max int
	s.db.QueryRow(ctx, `SELECT COUNT(*),(SELECT max_articles_per_user FROM issues WHERE id=$2) FROM articles WHERE user_id=$1 AND issue_id=$2`, userID, issueID).Scan(&count, &max)
	if count >= max { return nil, ErrSubmissionLimit }
	status := models.StatusPending
	if trusted { status = models.StatusLive }
	var html, pdf *string
	if req.ContentType == "editor" && req.ContentHTML != "" { html = &req.ContentHTML }
	if pdfURL != "" { pdf = &pdfURL }
	var a models.Article
	err := s.db.QueryRow(ctx, `
		INSERT INTO articles(user_id,issue_id,title,description,content_type,content_html,pdf_url,tag,status)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id,user_id,issue_id,title,description,content_type,content_html,pdf_url,tag,status,top_30,avg_rating,rating_count,review_count,created_at,updated_at`,
		userID, issueID, req.Title, nullStr(req.Description), req.ContentType, html, pdf, req.Tag, status,
	).Scan(&a.ID, &a.UserID, &a.IssueID, &a.Title, &a.Description, &a.ContentType,
		&a.ContentHTML, &a.PDFUrl, &a.Tag, &a.Status, &a.Top30, &a.AvgRating, &a.RatingCount,
		&a.ReviewCount, &a.CreatedAt, &a.UpdatedAt)
	return &a, err
}

func (s *ArticleService) List(ctx context.Context, p ArticleListParams) ([]models.Article, int, error) {
	if p.PageSize == 0 { p.PageSize = 20 }
	if p.Page == 0 { p.Page = 1 }
	order := "a.created_at DESC"
	if p.SortBy == "rating" { order = "a.avg_rating DESC" }
	where, args, n := "WHERE 1=1", []interface{}{}, 1
	if p.IssueID != nil { where += fmt.Sprintf(" AND a.issue_id=$%d", n); args = append(args, *p.IssueID); n++ }
	if p.Status != nil { where += fmt.Sprintf(" AND a.status=$%d", n); args = append(args, *p.Status); n++ } else { where += fmt.Sprintf(" AND a.status=$%d", n); args = append(args, models.StatusLive); n++ }
	if p.Tag != nil { where += fmt.Sprintf(" AND a.tag=$%d", n); args = append(args, *p.Tag); n++ }
	if p.Top30 != nil { where += fmt.Sprintf(" AND a.top_30=$%d", n); args = append(args, *p.Top30); n++ }
	var total int
	ca := make([]interface{}, len(args)); copy(ca, args)
	s.db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM articles a JOIN users u ON u.id=a.user_id %s`, where), ca...).Scan(&total)
	args = append(args, p.PageSize, (p.Page-1)*p.PageSize)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s %s %s ORDER BY %s LIMIT $%d OFFSET $%d`, aCols, aJoin, where, order, n, n+1), args...)
	if err != nil { return nil, 0, err }
	defer rows.Close()
	var list []models.Article
	for rows.Next() { var a models.Article; scanArticle(rows, &a); list = append(list, a) }
	return list, total, nil
}

func (s *ArticleService) ListByUser(ctx context.Context, userID uuid.UUID) ([]models.Article, error) {
	rows, err := s.db.Query(ctx, `SELECT `+aCols+` `+aJoin+` WHERE a.user_id=$1 ORDER BY a.created_at DESC`, userID)
	if err != nil { return nil, err }
	defer rows.Close()
	var list []models.Article
	for rows.Next() { var a models.Article; scanArticle(rows, &a); list = append(list, a) }
	return list, nil
}

func (s *ArticleService) Update(ctx context.Context, id, callerID uuid.UUID, req models.UpdateArticleRequest, isAdmin bool) (*models.Article, error) {
	a, err := s.GetByID(ctx, id)
	if err != nil { return nil, err }
	if !isAdmin && a.UserID != callerID { return nil, ErrNotOwner }
	if req.Title != nil { a.Title = *req.Title }
	if req.Description != nil { a.Description = req.Description }
	if req.ContentHTML != nil { a.ContentHTML = req.ContentHTML }
	s.db.Exec(ctx, `UPDATE articles SET title=$1,description=$2,content_html=$3,updated_at=now() WHERE id=$4`, a.Title, a.Description, a.ContentHTML, id)
	return s.GetByID(ctx, id)
}

func (s *ArticleService) Delete(ctx context.Context, id, callerID uuid.UUID, isAdmin bool) (*string, error) {
	a, err := s.GetByID(ctx, id)
	if err != nil { return nil, err }
	if !isAdmin && a.UserID != callerID { return nil, ErrNotOwner }
	s.db.Exec(ctx, `DELETE FROM articles WHERE id=$1`, id)
	return a.PDFUrl, nil
}

func (s *ArticleService) UpdateRatingStats(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE articles SET avg_rating=COALESCE((SELECT AVG(score) FROM ratings WHERE content_kind='article' AND content_id=$1),0),rating_count=(SELECT COUNT(*) FROM ratings WHERE content_kind='article' AND content_id=$1),updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *ArticleService) UpdateReviewCount(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE articles SET review_count=(SELECT COUNT(*) FROM reviews WHERE content_kind='article' AND content_id=$1),updated_at=now() WHERE id=$1`, id)
	return err
}

func (s *ArticleService) SetTop30(ctx context.Context, id uuid.UUID, val bool) error {
	_, err := s.db.Exec(ctx, `UPDATE articles SET top_30=$1,updated_at=now() WHERE id=$2`, val, id)
	return err
}

func (s *ArticleService) SetStatus(ctx context.Context, id uuid.UUID, st models.SubmissionStatus) error {
	_, err := s.db.Exec(ctx, `UPDATE articles SET status=$1,updated_at=now() WHERE id=$2`, st, id)
	return err
}

// ══════════════════════════════════════════════════════════════
// RatingService & ReviewService
// ══════════════════════════════════════════════════════════════

type RatingService struct {
	db         *pgxpool.Pool
	drawingSvc *DrawingService
	articleSvc *ArticleService
}

func NewRatingService(db *pgxpool.Pool, d *DrawingService, a *ArticleService) *RatingService {
	return &RatingService{db: db, drawingSvc: d, articleSvc: a}
}

func (s *RatingService) Rate(ctx context.Context, kind models.ContentKind, contentID uuid.UUID, score int16, userID *uuid.UUID, guestIP string) error {
	var err error
	if userID != nil {
		_, err = s.db.Exec(ctx, `
			INSERT INTO ratings(content_kind,content_id,user_id,score) VALUES($1,$2,$3,$4)
			ON CONFLICT (content_kind,content_id,user_id) WHERE user_id IS NOT NULL
			DO UPDATE SET score=EXCLUDED.score`, kind, contentID, userID, score)
	} else {
		_, err = s.db.Exec(ctx, `
			INSERT INTO ratings(content_kind,content_id,guest_ip,score) VALUES($1,$2,$3,$4)
			ON CONFLICT (content_kind,content_id,guest_ip) WHERE guest_ip IS NOT NULL
			DO UPDATE SET score=EXCLUDED.score`, kind, contentID, guestIP, score)
	}
	if err != nil { return err }
	switch kind {
	case models.KindDrawing: return s.drawingSvc.UpdateRatingStats(ctx, contentID)
	case models.KindArticle: return s.articleSvc.UpdateRatingStats(ctx, contentID)
	}
	return nil
}

type ReviewService struct {
	db         *pgxpool.Pool
	drawingSvc *DrawingService
	articleSvc *ArticleService
	notifSvc   *NotificationService
}

func NewReviewService(db *pgxpool.Pool, d *DrawingService, a *ArticleService, n *NotificationService) *ReviewService {
	return &ReviewService{db: db, drawingSvc: d, articleSvc: a, notifSvc: n}
}

func (s *ReviewService) ListForContent(ctx context.Context, kind models.ContentKind, contentID uuid.UUID) ([]models.Review, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id,r.content_kind,r.content_id,r.user_id,r.guest_name,r.body,r.flagged,r.created_at,u.kid_name
		FROM reviews r LEFT JOIN users u ON u.id=r.user_id
		WHERE r.content_kind=$1 AND r.content_id=$2 AND r.flagged=FALSE ORDER BY r.created_at DESC`,
		kind, contentID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []models.Review
	for rows.Next() {
		var rv models.Review
		rows.Scan(&rv.ID, &rv.ContentKind, &rv.ContentID, &rv.UserID, &rv.GuestName, &rv.Body, &rv.Flagged, &rv.CreatedAt, &rv.ReviewerName)
		rrows, _ := s.db.Query(ctx, `SELECT id,review_id,user_id,body,created_at FROM review_replies WHERE review_id=$1 ORDER BY created_at`, rv.ID)
		if rrows != nil {
			for rrows.Next() { var rep models.ReviewReply; rrows.Scan(&rep.ID, &rep.ReviewID, &rep.UserID, &rep.Body, &rep.CreatedAt); rv.Replies = append(rv.Replies, rep) }
			rrows.Close()
		}
		out = append(out, rv)
	}
	return out, nil
}

func (s *ReviewService) Create(ctx context.Context, kind models.ContentKind, contentID uuid.UUID, req models.CreateReviewRequest, userID *uuid.UUID, guestIP string) (*models.Review, error) {
	var rv models.Review
	var err error
	if userID != nil {
		err = s.db.QueryRow(ctx, `INSERT INTO reviews(content_kind,content_id,user_id,body) VALUES($1,$2,$3,$4) RETURNING id,content_kind,content_id,user_id,guest_name,body,flagged,created_at`,
			kind, contentID, userID, req.Body).Scan(&rv.ID, &rv.ContentKind, &rv.ContentID, &rv.UserID, &rv.GuestName, &rv.Body, &rv.Flagged, &rv.CreatedAt)
	} else {
		name := req.GuestName
		if name == "" { name = "Anonymous" }
		err = s.db.QueryRow(ctx, `INSERT INTO reviews(content_kind,content_id,guest_name,guest_ip,body) VALUES($1,$2,$3,$4,$5) RETURNING id,content_kind,content_id,user_id,guest_name,body,flagged,created_at`,
			kind, contentID, name, guestIP, req.Body).Scan(&rv.ID, &rv.ContentKind, &rv.ContentID, &rv.UserID, &rv.GuestName, &rv.Body, &rv.Flagged, &rv.CreatedAt)
	}
	if err != nil { return nil, err }
	switch kind {
	case models.KindDrawing:
		s.drawingSvc.UpdateReviewCount(ctx, contentID)
		if d, err2 := s.drawingSvc.GetByID(ctx, contentID); err2 == nil {
			go s.notifSvc.NotifyNewReview(ctx, d.UserID, "drawing", d.Title, contentID)
		}
	case models.KindArticle:
		s.articleSvc.UpdateReviewCount(ctx, contentID)
		if a, err2 := s.articleSvc.GetByID(ctx, contentID); err2 == nil {
			go s.notifSvc.NotifyNewReview(ctx, a.UserID, "article", a.Title, contentID)
		}
	}
	return &rv, nil
}

func (s *ReviewService) Reply(ctx context.Context, reviewID, callerID uuid.UUID, kind models.ContentKind, contentID uuid.UUID, req models.CreateReplyRequest) (*models.ReviewReply, error) {
	switch kind {
	case models.KindDrawing:
		d, err := s.drawingSvc.GetByID(ctx, contentID)
		if err != nil || d.UserID != callerID { return nil, ErrNotOwner }
	case models.KindArticle:
		a, err := s.articleSvc.GetByID(ctx, contentID)
		if err != nil || a.UserID != callerID { return nil, ErrNotOwner }
	}
	var rep models.ReviewReply
	err := s.db.QueryRow(ctx, `INSERT INTO review_replies(review_id,user_id,body) VALUES($1,$2,$3) RETURNING id,review_id,user_id,body,created_at`,
		reviewID, callerID, req.Body).Scan(&rep.ID, &rep.ReviewID, &rep.UserID, &rep.Body, &rep.CreatedAt)
	return &rep, err
}

func (s *ReviewService) FlagReview(ctx context.Context, id uuid.UUID, flagged bool) error {
	_, err := s.db.Exec(ctx, `UPDATE reviews SET flagged=$1 WHERE id=$2`, flagged, id); return err
}

func (s *ReviewService) DeleteReview(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM reviews WHERE id=$1`, id); return err
}

// ══════════════════════════════════════════════════════════════
// OrderService
// ══════════════════════════════════════════════════════════════

type OrderService struct{ db *pgxpool.Pool }

func NewOrderService(db *pgxpool.Pool) *OrderService { return &OrderService{db: db} }

const oCols = `o.id,o.user_id,o.issue_id,o.buyer_name,o.buyer_email,o.buyer_phone,o.format,o.copies,o.delivery_zone,o.delivery_address_line1,o.delivery_address_line2,o.delivery_city,o.delivery_district,o.delivery_fee,o.unit_price,o.subtotal,o.total,o.payment_method,o.status,o.paid_at,o.created_at,o.updated_at,i.name`
const oJoin = `FROM orders o JOIN issues i ON i.id=o.issue_id`

func scanOrder(row interface{ Scan(...interface{}) error }, o *models.Order) error {
	return row.Scan(&o.ID, &o.UserID, &o.IssueID, &o.BuyerName, &o.BuyerEmail, &o.BuyerPhone,
		&o.Format, &o.Copies, &o.DeliveryZone, &o.DeliveryAddressLine1, &o.DeliveryAddressLine2,
		&o.DeliveryCity, &o.DeliveryDistrict, &o.DeliveryFee,
		&o.UnitPrice, &o.Subtotal, &o.Total, &o.PaymentMethod, &o.Status, &o.PaidAt,
		&o.CreatedAt, &o.UpdatedAt, &o.IssueName)
}

func (s *OrderService) Create(ctx context.Context, req models.CreateOrderRequest, userID *uuid.UUID) (*models.Order, error) {
	issueID, err := uuid.Parse(req.IssueID)
	if err != nil { return nil, ErrIssueNotFound }

	var issueName string
	err = s.db.QueryRow(ctx, `SELECT name FROM issues WHERE id=$1`, issueID).Scan(&issueName)
	if errors.Is(err, pgx.ErrNoRows) { return nil, ErrIssueNotFound }
	if err != nil { return nil, err }

	format := models.OrderFormat(req.Format)
	copies := req.Copies
	if copies < 1 { copies = 1 }
	var unitPrice, deliveryFee int
	switch format {
	case models.FormatPhysical:
		unitPrice = models.PricePhysicalCents
		switch models.DeliveryZone(req.DeliveryZone) {
		case models.ZoneColombo: deliveryFee = models.DeliveryColomboCents
		case models.ZoneOutside: deliveryFee = models.DeliveryOutsideCents
		default: return nil, fmt.Errorf("delivery_zone required for physical orders")
		}
	case models.FormatPDF:
		unitPrice = models.PricePDFCents; deliveryFee = 0; copies = 1
	default:
		return nil, fmt.Errorf("invalid format: %s", req.Format)
	}
	subtotal := unitPrice * int(copies)
	total := subtotal + deliveryFee

	var dz *models.DeliveryZone
	var line1, line2, city, district, phone *string
	if format == models.FormatPhysical {
		v := models.DeliveryZone(req.DeliveryZone); dz = &v
		if req.DeliveryAddressLine1 != "" { line1 = &req.DeliveryAddressLine1 }
		if req.DeliveryAddressLine2 != "" { line2 = &req.DeliveryAddressLine2 }
		if req.DeliveryCity != "" { city = &req.DeliveryCity }
		if req.DeliveryDistrict != "" { district = &req.DeliveryDistrict }
	}
	if req.BuyerPhone != "" { phone = &req.BuyerPhone }

	var o models.Order
	err = s.db.QueryRow(ctx, `
		INSERT INTO orders(user_id,issue_id,buyer_name,buyer_email,buyer_phone,format,copies,
		                   delivery_zone,delivery_address_line1,delivery_address_line2,delivery_city,delivery_district,
		                   delivery_fee,unit_price,subtotal,total,payment_method,status)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,'pending')
		RETURNING `+oCols,
		userID, issueID, req.BuyerName, req.BuyerEmail, phone, format, copies,
		dz, line1, line2, city, district, deliveryFee, unitPrice, subtotal, total, req.PaymentMethod,
	).Scan(&o.ID, &o.UserID, &o.IssueID, &o.BuyerName, &o.BuyerEmail, &o.BuyerPhone,
		&o.Format, &o.Copies, &o.DeliveryZone, &o.DeliveryAddressLine1, &o.DeliveryAddressLine2,
		&o.DeliveryCity, &o.DeliveryDistrict, &o.DeliveryFee, &o.UnitPrice, &o.Subtotal, &o.Total,
		&o.PaymentMethod, &o.Status, &o.PaidAt, &o.CreatedAt, &o.UpdatedAt, &o.IssueName)
	return &o, err
}

func (s *OrderService) ListAdmin(ctx context.Context, issueID *uuid.UUID, format *string) ([]models.Order, error) {
	where, args, n := "WHERE 1=1", []interface{}{}, 1
	if issueID != nil { where += fmt.Sprintf(" AND o.issue_id=$%d", n); args = append(args, *issueID); n++ }
	if format != nil && *format != "" { where += fmt.Sprintf(" AND o.format=$%d", n); args = append(args, *format); n++ }
	_ = n
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s %s %s ORDER BY o.created_at DESC`, oCols, oJoin, where), args...)
	if err != nil { return nil, err }
	defer rows.Close()
	var list []models.Order
	for rows.Next() { var o models.Order; scanOrder(rows, &o); list = append(list, o) }
	return list, nil
}

func (s *OrderService) RevenueSummary(ctx context.Context) (*models.RevenueSummary, error) {
	var sum models.RevenueSummary
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(total) FILTER (WHERE status='paid'),0),
		       COUNT(*) FILTER (WHERE status='paid' AND format='physical'),
		       COUNT(*) FILTER (WHERE status='paid' AND format='pdf'),
		       COUNT(*) FILTER (WHERE status='pending')
		FROM orders`).Scan(&sum.TotalRevenueCents, &sum.PhysicalSalesCount, &sum.PDFSalesCount, &sum.PendingCount)
	return &sum, err
}

func (s *OrderService) RevenueByIssue(ctx context.Context) ([]models.RevenueByIssue, error) {
	rows, err := s.db.Query(ctx, `
		SELECT o.issue_id,i.name,
		       COALESCE(SUM(o.total) FILTER (WHERE o.format='physical' AND o.status='paid'),0),
		       COALESCE(SUM(o.total) FILTER (WHERE o.format='pdf' AND o.status='paid'),0),
		       COALESCE(SUM(o.total) FILTER (WHERE o.status='paid'),0)
		FROM orders o JOIN issues i ON i.id=o.issue_id
		GROUP BY o.issue_id,i.name ORDER BY 5 DESC NULLS LAST`)
	if err != nil { return nil, err }
	defer rows.Close()
	var list []models.RevenueByIssue
	for rows.Next() { var r models.RevenueByIssue; rows.Scan(&r.IssueID, &r.IssueName, &r.PhysicalCents, &r.PDFCents, &r.TotalCents); list = append(list, r) }
	return list, nil
}

func (s *OrderService) MarkPaid(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE orders SET status='paid',paid_at=now(),updated_at=now() WHERE id=$1`, id)
	return err
}

// ══════════════════════════════════════════════════════════════
// AdminService
// ══════════════════════════════════════════════════════════════

type AdminService struct {
	db         *pgxpool.Pool
	drawingSvc *DrawingService
	articleSvc *ArticleService
	notifSvc   *NotificationService
}

func NewAdminService(db *pgxpool.Pool, d *DrawingService, a *ArticleService, n *NotificationService) *AdminService {
	return &AdminService{db: db, drawingSvc: d, articleSvc: a, notifSvc: n}
}

func (s *AdminService) auditLog(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID uuid.UUID, detail interface{}) {
	var b []byte
	if detail != nil { b, _ = json.Marshal(detail) }
	s.db.Exec(ctx, `INSERT INTO audit_log(admin_id,action,target_type,target_id,detail_json) VALUES($1,$2,$3,$4,$5)`,
		adminID, action, targetType, targetID, b)
}

func (s *AdminService) ApproveDrawing(ctx context.Context, adminID, id uuid.UUID) error {
	d, err := s.drawingSvc.GetByID(ctx, id)
	if err != nil { return err }
	if err := s.drawingSvc.SetStatus(ctx, id, models.StatusLive); err != nil { return err }
	s.auditLog(ctx, adminID, "approved_drawing", "drawing", id, nil)
	go s.notifSvc.NotifyAdminEdited(ctx, d.UserID, "drawing", d.Title, id)
	return nil
}

func (s *AdminService) RejectDrawing(ctx context.Context, adminID, id uuid.UUID) error {
	d, err := s.drawingSvc.GetByID(ctx, id)
	if err != nil { return err }
	s.drawingSvc.SetStatus(ctx, id, models.StatusRejected)
	s.auditLog(ctx, adminID, "rejected_drawing", "drawing", id, nil)
	go s.notifSvc.NotifyAdminDeleted(ctx, d.UserID, "drawing", d.Title)
	return nil
}

func (s *AdminService) ApproveArticle(ctx context.Context, adminID, id uuid.UUID) error {
	a, err := s.articleSvc.GetByID(ctx, id)
	if err != nil { return err }
	s.articleSvc.SetStatus(ctx, id, models.StatusLive)
	s.auditLog(ctx, adminID, "approved_article", "article", id, nil)
	go s.notifSvc.NotifyAdminEdited(ctx, a.UserID, "article", a.Title, id)
	return nil
}

func (s *AdminService) SetDrawingTop30(ctx context.Context, adminID, id uuid.UUID, val bool) error {
	d, err := s.drawingSvc.GetByID(ctx, id)
	if err != nil { return err }
	s.drawingSvc.SetTop30(ctx, id, val)
	action := "added_to_top30"
	if !val { action = "removed_from_top30" }
	s.auditLog(ctx, adminID, action, "drawing", id, nil)
	if val {
		var issueName string
		s.db.QueryRow(ctx, `SELECT name FROM issues WHERE id=$1`, d.IssueID).Scan(&issueName)
		go s.notifSvc.NotifyTop30Selected(ctx, d.UserID, "drawing", d.Title, issueName, id)
	}
	return nil
}

func (s *AdminService) SetArticleTop30(ctx context.Context, adminID, id uuid.UUID, val bool) error {
	a, err := s.articleSvc.GetByID(ctx, id)
	if err != nil { return err }
	s.articleSvc.SetTop30(ctx, id, val)
	action := "added_to_top30"
	if !val { action = "removed_from_top30" }
	s.auditLog(ctx, adminID, action, "article", id, nil)
	if val {
		var issueName string
		s.db.QueryRow(ctx, `SELECT name FROM issues WHERE id=$1`, a.IssueID).Scan(&issueName)
		go s.notifSvc.NotifyTop30Selected(ctx, a.UserID, "article", a.Title, issueName, id)
	}
	return nil
}

func (s *AdminService) GetTop3Suggestions(ctx context.Context, issueID uuid.UUID) ([]models.Top3Contributor, error) {
	rows, err := s.db.Query(ctx, `
		WITH scores AS (
			SELECT u.id, u.kid_name, u.school_name, u.avatar_url, u.age_group,
			       ROUND((COALESCE(AVG(d.avg_rating) FILTER (WHERE d.issue_id=$1 AND d.top_30),0)*0.6 +
			              COALESCE(AVG(a.avg_rating) FILTER (WHERE a.issue_id=$1 AND a.top_30),0)*0.4)::numeric,2) AS score
			FROM users u
			LEFT JOIN drawings d ON d.user_id=u.id
			LEFT JOIN articles a ON a.user_id=u.id
			WHERE u.role='user' AND u.active=TRUE
			GROUP BY u.id,u.kid_name,u.school_name,u.avatar_url,u.age_group
			HAVING COUNT(d.id) FILTER (WHERE d.top_30) > 0 OR COUNT(a.id) FILTER (WHERE a.top_30) > 0
		), ranked AS (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY age_group ORDER BY score DESC) AS rank FROM scores
		)
		SELECT id,kid_name,school_name,avatar_url,age_group,score,rank,
		       (SELECT image_url FROM drawings WHERE user_id=ranked.id AND top_30 AND issue_id=$1 ORDER BY avg_rating DESC LIMIT 1)
		FROM ranked WHERE rank <= 3 ORDER BY age_group, rank`, issueID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []models.Top3Contributor
	for rows.Next() {
		var t models.Top3Contributor
		t.IssueID = issueID; t.SuggestedBySystem = true
		rows.Scan(&t.UserID, &t.ContributorName, &t.ContributorSchool, &t.ContributorAvatar,
			&t.AgeGroup, &t.AggregateScore, &t.Rank, &t.HighlightImageURL)
		out = append(out, t)
	}
	return out, nil
}

func (s *AdminService) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := s.db.Query(ctx, `SELECT id,email,role,trusted,active,language_pref,kid_name,age,age_group,school_name,guardian_name,contact_number,avatar_url,bio,profile_complete,created_at,updated_at FROM users ORDER BY created_at DESC`)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		var u models.User
		rows.Scan(&u.ID, &u.Email, &u.Role, &u.Trusted, &u.Active, &u.LanguagePref,
			&u.KidName, &u.Age, &u.AgeGroup, &u.SchoolName, &u.GuardianName, &u.ContactNumber,
			&u.AvatarURL, &u.Bio, &u.ProfileComplete, &u.CreatedAt, &u.UpdatedAt)
		out = append(out, u)
	}
	return out, nil
}

func (s *AdminService) SetTrusted(ctx context.Context, adminID, userID uuid.UUID, val bool) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET trusted=$1,updated_at=now() WHERE id=$2`, val, userID)
	action := "trusted_user"; if !val { action = "revoked_trust" }
	s.auditLog(ctx, adminID, action, "user", userID, nil)
	return err
}

func (s *AdminService) SetActive(ctx context.Context, adminID, userID uuid.UUID, val bool) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET active=$1,updated_at=now() WHERE id=$2`, val, userID)
	action := "activated_user"; if !val { action = "deactivated_user" }
	s.auditLog(ctx, adminID, action, "user", userID, nil)
	return err
}

func (s *AdminService) ListAuditLog(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if limit == 0 { limit = 100 }
	rows, err := s.db.Query(ctx, `SELECT al.id,al.admin_id,al.action,al.target_type,al.target_id,al.detail_json,al.created_at,u.email FROM audit_log al JOIN users u ON u.id=al.admin_id ORDER BY al.created_at DESC LIMIT $1`, limit)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, adminID uuid.UUID; var action, targetType string; var targetID *uuid.UUID; var detail []byte; var createdAt interface{}; var email string
		rows.Scan(&id, &adminID, &action, &targetType, &targetID, &detail, &createdAt, &email)
		row := map[string]interface{}{"id": id, "admin_id": adminID, "action": action, "target_type": targetType, "target_id": targetID, "created_at": createdAt, "admin_email": email}
		if detail != nil { var d interface{}; json.Unmarshal(detail, &d); row["detail"] = d }
		out = append(out, row)
	}
	return out, nil
}

func (s *AdminService) ListContactMessages(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.db.Query(ctx, `SELECT id,name,email,message,status,reply_body,replied_at,created_at FROM contact_messages ORDER BY created_at DESC`)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id uuid.UUID; var name, email, message, status string; var replyBody *string; var repliedAt, createdAt interface{}
		rows.Scan(&id, &name, &email, &message, &status, &replyBody, &repliedAt, &createdAt)
		out = append(out, map[string]interface{}{"id": id, "name": name, "email": email, "message": message, "status": status, "reply_body": replyBody, "replied_at": repliedAt, "created_at": createdAt})
	}
	return out, nil
}

func (s *AdminService) CreateContactMessage(ctx context.Context, name, email, message string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO contact_messages(name,email,message) VALUES($1,$2,$3)`, name, email, message)
	return err
}

func (s *AdminService) ReplyContact(ctx context.Context, adminID, msgID uuid.UUID, reply string) error {
	_, err := s.db.Exec(ctx, `UPDATE contact_messages SET status='replied',reply_body=$1,replied_at=now(),replied_by=$2 WHERE id=$3`, reply, adminID, msgID)
	return err
}

func (s *AdminService) GetContent(ctx context.Context, key, lang string) (map[string]interface{}, error) {
	var b []byte
	s.db.QueryRow(ctx, `SELECT content_json FROM site_content WHERE key=$1 AND language=$2`, key, lang).Scan(&b)
	out := map[string]interface{}{}
	if b != nil { json.Unmarshal(b, &out) }
	return out, nil
}

func (s *AdminService) SetContent(ctx context.Context, key, lang string, content map[string]interface{}) error {
	b, _ := json.Marshal(content)
	_, err := s.db.Exec(ctx, `INSERT INTO site_content(key,language,content_json) VALUES($1,$2,$3) ON CONFLICT(key,language) DO UPDATE SET content_json=$3,updated_at=now()`, key, lang, b)
	return err
}

// ══════════════════════════════════════════════════════════════
// BootstrapAdmin
// ══════════════════════════════════════════════════════════════

func BootstrapAdmin(ctx context.Context, db *pgxpool.Pool, email, password string) error {
	var exists bool
	db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, email).Scan(&exists)
	if exists { return nil }
	hash, err := auth.HashPassword(password)
	if err != nil { return err }
	_, err = db.Exec(ctx, `INSERT INTO users(email,password_hash,role,trusted,active,profile_complete) VALUES($1,$2,'admin',TRUE,TRUE,TRUE)`, email, hash)
	if err != nil { return err }
	log.Printf("✓ Admin bootstrapped: %s", email)
	return nil
}

// ══════════════════════════════════════════════════════════════
// SearchService
// ══════════════════════════════════════════════════════════════

type SearchService struct{ db *pgxpool.Pool }

func NewSearchService(db *pgxpool.Pool) *SearchService { return &SearchService{db: db} }

type SearchResult struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	ImageURL  *string `json:"image_url,omitempty"`
	Author    *string `json:"author,omitempty"`
	IssueName *string `json:"issue_name,omitempty"`
	AvgRating float64 `json:"avg_rating"`
}

func (s *SearchService) Search(ctx context.Context, q string) ([]SearchResult, error) {
	pattern := fmt.Sprintf("%%%s%%", q)
	rows, err := s.db.Query(ctx, `
		SELECT 'drawing' AS kind, d.id::text, d.title, d.image_url, u.kid_name, i.name, d.avg_rating
		FROM drawings d
		JOIN users  u ON u.id = d.user_id
		JOIN issues i ON i.id = d.issue_id
		WHERE d.status = 'live'
		  AND (d.title ILIKE $1 OR u.kid_name ILIKE $1 OR u.school_name ILIKE $1)
		UNION ALL
		SELECT 'article', a.id::text, a.title, NULL, u.kid_name, i.name, a.avg_rating
		FROM articles a
		JOIN users  u ON u.id = a.user_id
		JOIN issues i ON i.id = a.issue_id
		WHERE a.status = 'live'
		  AND (a.title ILIKE $1 OR u.kid_name ILIKE $1)
		ORDER BY avg_rating DESC LIMIT 40
	`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		rows.Scan(&r.Kind, &r.ID, &r.Title, &r.ImageURL, &r.Author, &r.IssueName, &r.AvgRating)
		out = append(out, r)
	}
	return out, nil
}

// ConfirmTop3Entry upserts a single top-3 selection (called per entry from handler).
func (s *AdminService) ConfirmTop3Entry(ctx context.Context, adminID, issueID, userID uuid.UUID, ageGroup models.AgeGroup, rank int16, score float64) (uuid.UUID, error) {
	var top3ID uuid.UUID
	err := s.db.QueryRow(ctx, `
		INSERT INTO top3_contributors(issue_id,user_id,age_group,rank,aggregate_score,confirmed_by_admin,confirmed_at)
		VALUES($1,$2,$3,$4,$5,TRUE,now())
		ON CONFLICT(issue_id,age_group,rank)
		DO UPDATE SET user_id=$2,aggregate_score=$5,confirmed_by_admin=TRUE,confirmed_at=now()
		RETURNING id
	`, issueID, userID, ageGroup, rank, score).Scan(&top3ID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("confirming top3: %w", err)
	}
	var issueName string
	s.db.QueryRow(ctx, `SELECT name FROM issues WHERE id=$1`, issueID).Scan(&issueName)
	go s.notifSvc.NotifyTop3Confirmed(ctx, userID, issueName, top3ID)
	s.auditLog(ctx, adminID, "confirmed_top3", "top3", top3ID, nil)
	return top3ID, nil
}
