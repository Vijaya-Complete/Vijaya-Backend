// Package handlers contains all HTTP handlers for the Vijaya API.
// Each sub-package registers its own routes via RegisterRoutes.
package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"vijaya-backend/internal/httpx"
	"vijaya-backend/internal/middleware"
	"vijaya-backend/internal/models"
	"vijaya-backend/internal/services"
	"vijaya-backend/internal/storage"
)

// ── helpers ───────────────────────────────────────────────────

func qInt(c echo.Context, key string, def int) int {
	if v := c.QueryParam(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func extFrom(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i:]
		}
	}
	return ".bin"
}

func readFile(c echo.Context, field string, maxMB int64) ([]byte, string, error) {
	file, err := c.FormFile(field)
	if err != nil {
		return nil, "", err
	}
	if file.Size > maxMB<<20 {
		return nil, "", fmt.Errorf("file too large")
	}
	src, err := file.Open()
	if err != nil {
		return nil, "", err
	}
	defer src.Close()
	data := make([]byte, file.Size)
	src.Read(data)
	return data, file.Header.Get("Content-Type"), nil
}

// ══════════════════════════════════════════════════════════════
// AuthHandler
// ══════════════════════════════════════════════════════════════

type AuthHandler struct {
	auth  *services.AuthService
	email *services.EmailService
}

func NewAuthHandler(a *services.AuthService, e *services.EmailService) *AuthHandler {
	return &AuthHandler{auth: a, email: e}
}

func (h *AuthHandler) Register(g *echo.Group, authMW echo.MiddlewareFunc) {
	g.POST("/register", h.register)
	g.POST("/login", h.login)
	g.POST("/refresh", h.refresh)
	g.POST("/logout", h.logout)
	g.POST("/forgot-password", h.forgotPassword)
	g.POST("/reset-password", h.resetPassword)
	g.POST("/complete-profile", h.completeProfile, authMW)
}

func (h *AuthHandler) register(c echo.Context) error {
	var req models.RegisterRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	if !req.AgeConfirmation {
		return httpx.Error(c, http.StatusBadRequest, "AGE_CONFIRMATION_REQUIRED", "Guardian permission required")
	}
	user, err := h.auth.Register(c.Request().Context(), req)
	if err != nil {
		if err == services.ErrEmailTaken {
			return httpx.Conflict(c, "EMAIL_TAKEN", "This email is already registered")
		}
		c.Logger().Error(err)
		return httpx.Internal(c)
	}
	return httpx.Created(c, user)
}

func (h *AuthHandler) login(c echo.Context) error {
	var req models.LoginRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	user, access, refresh, err := h.auth.Login(c.Request().Context(), req.Email, req.Password)
	if err != nil {
		switch err {
		case services.ErrInvalidCredentials:
			return httpx.Error(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Incorrect email or password")
		case services.ErrAccountDeactivated:
			return httpx.Error(c, http.StatusForbidden, "ACCOUNT_DEACTIVATED", "Account has been deactivated")
		default:
			c.Logger().Error(err)
			return httpx.Internal(c)
		}
	}
	return httpx.OK(c, models.AuthResponse{AccessToken: access, RefreshToken: refresh, User: *user})
}

func (h *AuthHandler) refresh(c echo.Context) error {
	var req models.RefreshRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	access, refresh, err := h.auth.Refresh(c.Request().Context(), req.RefreshToken)
	if err != nil {
		return httpx.Error(c, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "Please log in again")
	}
	return httpx.OK(c, map[string]string{"access_token": access, "refresh_token": refresh})
}

func (h *AuthHandler) logout(c echo.Context) error {
	var req models.RefreshRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	h.auth.Logout(c.Request().Context(), req.RefreshToken)
	return httpx.NoContent(c)
}

func (h *AuthHandler) forgotPassword(c echo.Context) error {
	var req models.ForgotPasswordRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	token, user, err := h.auth.ForgotPassword(c.Request().Context(), req.Email)
	if err != nil {
		c.Logger().Error(err)
		return httpx.Internal(c)
	}
	if user != nil {
		go h.email.SendPasswordReset(user.Email, token)
	}
	return httpx.OK(c, map[string]string{"message": "If that email exists, a reset link has been sent"})
}

func (h *AuthHandler) resetPassword(c echo.Context) error {
	var req models.ResetPasswordRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	if err := h.auth.ResetPassword(c.Request().Context(), req.Token, req.NewPassword); err != nil {
		return httpx.Error(c, http.StatusBadRequest, "INVALID_RESET_TOKEN", "Invalid or expired reset link")
	}
	return httpx.OK(c, map[string]string{"message": "Password updated successfully"})
}

func (h *AuthHandler) completeProfile(c echo.Context) error {
	userID, _ := middleware.UserIDFromContext(c)
	var req models.CompleteProfileRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	user, err := h.auth.CompleteProfile(c.Request().Context(), userID, req)
	if err != nil {
		c.Logger().Error(err)
		return httpx.Internal(c)
	}
	return httpx.OK(c, user)
}

// ══════════════════════════════════════════════════════════════
// UserHandler
// ══════════════════════════════════════════════════════════════

type UserHandler struct {
	userSvc  *services.UserService
	notifSvc *services.NotificationService
	store    *storage.Client
}

func NewUserHandler(u *services.UserService, n *services.NotificationService, s *storage.Client) *UserHandler {
	return &UserHandler{u, n, s}
}

func (h *UserHandler) Register(g *echo.Group, authMW echo.MiddlewareFunc) {
	me := g.Group("/users/me", authMW)
	me.GET("", h.getMe)
	me.PUT("", h.updateMe)
	me.DELETE("", h.deleteMe)
	me.PUT("/avatar", h.updateAvatar)
	me.PUT("/language", h.updateLanguage)
	me.GET("/submissions/summary", h.submissionsSummary)
	me.GET("/notifications", h.getNotifications)
	me.PUT("/notifications/read-all", h.markAllRead)
	me.GET("/drawings", h.myDrawings)
	me.GET("/articles", h.myArticles)
}

func (h *UserHandler) getMe(c echo.Context) error {
	id, _ := middleware.UserIDFromContext(c)
	u, err := h.userSvc.GetByID(c.Request().Context(), id)
	if err != nil {
		return httpx.NotFound(c, "User")
	}
	return httpx.OK(c, u)
}

func (h *UserHandler) updateMe(c echo.Context) error {
	id, _ := middleware.UserIDFromContext(c)
	var req models.UpdateProfileRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	u, err := h.userSvc.UpdateProfile(c.Request().Context(), id, req)
	if err != nil {
		c.Logger().Error(err)
		return httpx.Internal(c)
	}
	return httpx.OK(c, u)
}

func (h *UserHandler) deleteMe(c echo.Context) error {
	id, _ := middleware.UserIDFromContext(c)
	if err := h.userSvc.DeleteAccount(c.Request().Context(), id); err != nil {
		return httpx.Internal(c)
	}
	return httpx.NoContent(c)
}

func (h *UserHandler) updateAvatar(c echo.Context) error {
	id, _ := middleware.UserIDFromContext(c)
	file, err := c.FormFile("avatar")
	if err != nil {
		return httpx.BadRequest(c, "avatar file is required")
	}
	if file.Size > 5<<20 {
		return httpx.BadRequest(c, "Avatar must be under 5MB")
	}
	src, _ := file.Open()
	defer src.Close()
	data := make([]byte, file.Size)
	src.Read(data)
	key := fmt.Sprintf("avatars/%s%s", id, extFrom(file.Filename))
	url, err := h.store.Upload(c.Request().Context(), key, data, file.Header.Get("Content-Type"))
	if err != nil {
		c.Logger().Error(err)
		return httpx.Internal(c)
	}
	h.userSvc.UpdateAvatar(c.Request().Context(), id, url)
	return httpx.OK(c, map[string]string{"avatar_url": url})
}

func (h *UserHandler) updateLanguage(c echo.Context) error {
	id, _ := middleware.UserIDFromContext(c)
	var req struct {
		Language string `json:"language" validate:"required,oneof=en si ta"`
	}
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	lang := models.LanguageCode(req.Language)
	u, err := h.userSvc.UpdateProfile(c.Request().Context(), id, models.UpdateProfileRequest{LanguagePref: (*string)(&lang)})
	if err != nil {
		return httpx.Internal(c)
	}
	return httpx.OK(c, u)
}

func (h *UserHandler) submissionsSummary(c echo.Context) error {
	id, _ := middleware.UserIDFromContext(c)
	sum, err := h.userSvc.GetSubmissionsSummary(c.Request().Context(), id)
	if err != nil {
		return httpx.Internal(c)
	}
	return httpx.OK(c, sum)
}

func (h *UserHandler) getNotifications(c echo.Context) error {
	id, _ := middleware.UserIDFromContext(c)
	notifs, err := h.notifSvc.ListForUser(c.Request().Context(), id)
	if err != nil {
		return httpx.Internal(c)
	}
	return httpx.OK(c, notifs)
}

func (h *UserHandler) markAllRead(c echo.Context) error {
	id, _ := middleware.UserIDFromContext(c)
	h.notifSvc.MarkAllRead(c.Request().Context(), id)
	return httpx.NoContent(c)
}

// myDrawings and myArticles are wired by DrawingHandler / ArticleHandler
// These stubs satisfy the router but delegate to the correct service.
func (h *UserHandler) myDrawings(c echo.Context) error {
	return httpx.OK(c, []interface{}{}) // overridden by DrawingHandler.Register
}
func (h *UserHandler) myArticles(c echo.Context) error {
	return httpx.OK(c, []interface{}{}) // overridden by ArticleHandler.Register
}

// ══════════════════════════════════════════════════════════════
// IssueHandler
// ══════════════════════════════════════════════════════════════

type IssueHandler struct{ svc *services.IssueService }

func NewIssueHandler(s *services.IssueService) *IssueHandler { return &IssueHandler{s} }

func (h *IssueHandler) Register(g *echo.Group, authMW, adminMW echo.MiddlewareFunc) {
	g.GET("/issues", h.list)
	g.GET("/issues/active", h.getActive)
	g.GET("/issues/:id", h.getByID)
	g.POST("/admin/issues", h.create, authMW, adminMW)
	g.PUT("/admin/issues/:id", h.update, authMW, adminMW)
	g.PUT("/admin/issues/:id/status", h.setStatus, authMW, adminMW)
}

func (h *IssueHandler) list(c echo.Context) error {
	issues, err := h.svc.List(c.Request().Context())
	if err != nil {
		return httpx.Internal(c)
	}
	return httpx.OK(c, issues)
}
func (h *IssueHandler) getActive(c echo.Context) error {
	issue, err := h.svc.GetActive(c.Request().Context())
	if err != nil {
		return httpx.NotFound(c, "Active issue")
	}
	return httpx.OK(c, issue)
}
func (h *IssueHandler) getByID(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return httpx.BadRequest(c, "Invalid issue ID")
	}
	issue, err := h.svc.GetByID(c.Request().Context(), id)
	if err != nil {
		return httpx.NotFound(c, "Issue")
	}
	return httpx.OK(c, issue)
}
func (h *IssueHandler) create(c echo.Context) error {
	var req models.CreateIssueRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	issue, err := h.svc.Create(c.Request().Context(), req)
	if err != nil {
		c.Logger().Error(err)
		return httpx.Internal(c)
	}
	return httpx.Created(c, issue)
}
func (h *IssueHandler) update(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return httpx.BadRequest(c, "Invalid issue ID")
	}
	var req models.CreateIssueRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	issue, err := h.svc.Update(c.Request().Context(), id, req)
	if err != nil {
		if err == services.ErrIssueNotFound {
			return httpx.NotFound(c, "Issue")
		}
		return httpx.Internal(c)
	}
	return httpx.OK(c, issue)
}
func (h *IssueHandler) setStatus(c echo.Context) error {
	id, _ := uuid.Parse(c.Param("id"))
	var body struct {
		Status string `json:"status" validate:"required,oneof=open closed published"`
	}
	if err := httpx.BindAndValidate(c, &body); err != nil {
		return err
	}
	h.svc.SetStatus(c.Request().Context(), id, models.IssueStatus(body.Status))
	return httpx.OK(c, map[string]string{"status": body.Status})
}

// ══════════════════════════════════════════════════════════════
// DrawingHandler
// ══════════════════════════════════════════════════════════════

type DrawingHandler struct {
	svc      *services.DrawingService
	userSvc  *services.UserService
	notifSvc *services.NotificationService
	store    *storage.Client
}

func NewDrawingHandler(d *services.DrawingService, u *services.UserService, n *services.NotificationService, s *storage.Client) *DrawingHandler {
	return &DrawingHandler{d, u, n, s}
}

func (h *DrawingHandler) Register(g *echo.Group, authMW, optionalMW echo.MiddlewareFunc) {
	g.GET("/drawings", h.list, optionalMW)
	g.GET("/drawings/:id", h.getByID, optionalMW)
	g.POST("/drawings", h.create, authMW)
	g.PUT("/drawings/:id", h.update, authMW)
	g.PUT("/drawings/:id/image", h.updateImage, authMW)
	g.DELETE("/drawings/:id", h.delete, authMW)
	g.GET("/users/me/drawings", h.listMine, authMW)
}

func (h *DrawingHandler) list(c echo.Context) error {
	p := services.DrawingListParams{
		SortBy: c.QueryParam("sort"), Page: qInt(c, "page", 1), PageSize: qInt(c, "page_size", 20),
	}
	if id := c.QueryParam("issue_id"); id != "" {
		if uid, err := uuid.Parse(id); err == nil {
			p.IssueID = &uid
		}
	}
	if ag := c.QueryParam("age_group"); ag != "" {
		v := models.AgeGroup(ag); p.AgeGroup = &v
	}
	if c.QueryParam("top30") == "true" {
		v := true; p.Top30 = &v
	}
	list, total, err := h.svc.List(c.Request().Context(), p)
	if err != nil {
		return httpx.Internal(c)
	}
	return httpx.OK(c, map[string]interface{}{"items": list, "total": total, "page": p.Page})
}

func (h *DrawingHandler) getByID(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return httpx.BadRequest(c, "Invalid drawing ID")
	}
	d, err := h.svc.GetByID(c.Request().Context(), id)
	if err != nil {
		return httpx.NotFound(c, "Drawing")
	}
	return httpx.OK(c, d)
}

func (h *DrawingHandler) create(c echo.Context) error {
	userID, _ := middleware.UserIDFromContext(c)
	role, _ := c.Get(middleware.CtxRole).(string)

	var req models.CreateDrawingRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	issueID, err := uuid.Parse(req.IssueID)
	if err != nil {
		return httpx.BadRequest(c, "Invalid issue_id")
	}

	data, ct, err := readFile(c, "image", 10)
	if err != nil {
		return httpx.BadRequest(c, "image file required and must be under 10MB")
	}

	key := fmt.Sprintf("drawings/%s/%s%s", userID, uuid.NewString(), extFrom(c.Request().FormValue("filename")))
	imageURL, err := h.store.Upload(c.Request().Context(), key, data, ct)
	if err != nil {
		c.Logger().Error(err)
		return httpx.Internal(c)
	}

	user, _ := h.userSvc.GetByID(c.Request().Context(), userID)
	trusted := (user != nil && user.Trusted) || role == "admin"

	drawing, err := h.svc.Create(c.Request().Context(), userID, issueID, req, imageURL, trusted)
	if err != nil {
		if err == services.ErrSubmissionLimit {
			return httpx.Error(c, http.StatusConflict, "SUBMISSION_LIMIT", "Submission limit reached for this issue")
		}
		c.Logger().Error(err)
		return httpx.Internal(c)
	}
	return httpx.Created(c, drawing)
}

func (h *DrawingHandler) update(c echo.Context) error {
	callerID, _ := middleware.UserIDFromContext(c)
	role, _ := c.Get(middleware.CtxRole).(string)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return httpx.BadRequest(c, "Invalid drawing ID")
	}
	var req models.UpdateDrawingRequest
	if err := httpx.BindAndValidate(c, &req); err != nil {
		return err
	}
	d, err := h.svc.Update(c.Request().Context(), id, callerID, req, role == "admin")
	if err != nil {
		switch err {
		case services.ErrDrawingNotFound:
			return httpx.NotFound(c, "Drawing")
		case services.ErrNotOwner:
			return httpx.Forbidden(c, "You do not own this drawing")
		default:
			return httpx.Internal(c)
		}
	}
	return httpx.OK(c, d)
}

func (h *DrawingHandler) updateImage(c echo.Context) error {
	callerID, _ := middleware.UserIDFromContext(c)
	role, _ := c.Get(middleware.CtxRole).(string)
	id, _ := uuid.Parse(c.Param("id"))

	data, ct, err := readFile(c, "image", 10)
	if err != nil {
		return httpx.BadRequest(c, "image file required and must be under 10MB")
	}
	key := fmt.Sprintf("drawings/%s/%s.img", callerID, uuid.NewString())
	url, err := h.store.Upload(c.Request().Context(), key, data, ct)
	if err != nil {
		return httpx.Internal(c)
	}
	if err := h.svc.UpdateImage(c.Request().Context(), id, callerID, url, role == "admin"); err != nil {
		if err == services.ErrNotOwner {
			return httpx.Forbidden(c, "You do not own this drawing")
		}
		return httpx.Internal(c)
	}
	return httpx.OK(c, map[string]string{"image_url": url})
}

func (h *DrawingHandler) delete(c echo.Context) error {
	callerID, _ := middleware.UserIDFromContext(c)
	role, _ := c.Get(middleware.CtxRole).(string)
	id, _ := uuid.Parse(c.Param("id"))

	oldURL, err := h.svc.Delete(c.Request().Context(), id, callerID, role == "admin")
	if err != nil {
		switch err {
		case services.ErrDrawingNotFound:
			return httpx.NotFound(c, "Drawing")
		case services.ErrNotOwner:
			return httpx.Forbidden(c, "You do not own this drawing")
		default:
			return httpx.Internal(c)
		}
	}
	if oldURL != "" {
		go h.store.Delete(c.Request().Context(), storage.StorageKey(oldURL, h.store.PublicURL))
	}
	return httpx.NoContent(c)
}

func (h *DrawingHandler) listMine(c echo.Context) error {
	userID, _ := middleware.UserIDFromContext(c)
	list, err := h.svc.ListByUser(c.Request().Context(), userID)
	if err != nil {
		return httpx.Internal(c)
	}
	return httpx.OK(c, list)
}

// ══════════════════════════════════════════════════════════════
// ArticleHandler
// ══════════════════════════════════════════════════════════════

type ArticleHandler struct {
	svc      *services.ArticleService
	userSvc  *services.UserService
	notifSvc *services.NotificationService
	store    *storage.Client
}

func NewArticleHandler(a *services.ArticleService, u *services.UserService, n *services.NotificationService, s *storage.Client) *ArticleHandler {
	return &ArticleHandler{a, u, n, s}
}

func (h *ArticleHandler) Register(g *echo.Group, authMW, optionalMW echo.MiddlewareFunc) {
	g.GET("/articles", h.list, optionalMW)
	g.GET("/articles/:id", h.getByID, optionalMW)
	g.POST("/articles", h.create, authMW)
	g.PUT("/articles/:id", h.update, authMW)
	g.DELETE("/articles/:id", h.delete, authMW)
	g.GET("/users/me/articles", h.listMine, authMW)
}

func (h *ArticleHandler) list(c echo.Context) error {
	p := services.ArticleListParams{SortBy: c.QueryParam("sort"), Page: qInt(c, "page", 1), PageSize: qInt(c, "page_size", 20)}
	if id := c.QueryParam("issue_id"); id != "" {
		if uid, err := uuid.Parse(id); err == nil { p.IssueID = &uid }
	}
	if tag := c.QueryParam("tag"); tag != "" { t := models.StoryTag(tag); p.Tag = &t }
	if c.QueryParam("top30") == "true" { v := true; p.Top30 = &v }
	list, total, err := h.svc.List(c.Request().Context(), p)
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, map[string]interface{}{"items": list, "total": total, "page": p.Page})
}

func (h *ArticleHandler) getByID(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil { return httpx.BadRequest(c, "Invalid article ID") }
	a, err := h.svc.GetByID(c.Request().Context(), id)
	if err != nil { return httpx.NotFound(c, "Article") }
	return httpx.OK(c, a)
}

func (h *ArticleHandler) create(c echo.Context) error {
	userID, _ := middleware.UserIDFromContext(c)
	role, _ := c.Get(middleware.CtxRole).(string)
	var req models.CreateArticleRequest
	if err := httpx.BindAndValidate(c, &req); err != nil { return err }
	issueID, err := uuid.Parse(req.IssueID)
	if err != nil { return httpx.BadRequest(c, "Invalid issue_id") }
	var pdfURL string
	if data, ct, err := readFile(c, "pdf", 20); err == nil {
		key := fmt.Sprintf("articles/%s/%s.pdf", userID, uuid.NewString())
		pdfURL, _ = h.store.Upload(c.Request().Context(), key, data, ct)
	}
	user, _ := h.userSvc.GetByID(c.Request().Context(), userID)
	trusted := (user != nil && user.Trusted) || role == "admin"
	a, err := h.svc.Create(c.Request().Context(), userID, issueID, req, pdfURL, trusted)
	if err != nil {
		if err == services.ErrSubmissionLimit { return httpx.Error(c, http.StatusConflict, "SUBMISSION_LIMIT", "Submission limit reached") }
		c.Logger().Error(err); return httpx.Internal(c)
	}
	return httpx.Created(c, a)
}

func (h *ArticleHandler) update(c echo.Context) error {
	callerID, _ := middleware.UserIDFromContext(c)
	role, _ := c.Get(middleware.CtxRole).(string)
	id, _ := uuid.Parse(c.Param("id"))
	var req models.UpdateArticleRequest
	if err := httpx.BindAndValidate(c, &req); err != nil { return err }
	a, err := h.svc.Update(c.Request().Context(), id, callerID, req, role == "admin")
	if err != nil {
		switch err {
		case services.ErrArticleNotFound: return httpx.NotFound(c, "Article")
		case services.ErrNotOwner: return httpx.Forbidden(c, "You do not own this article")
		default: return httpx.Internal(c)
		}
	}
	return httpx.OK(c, a)
}

func (h *ArticleHandler) delete(c echo.Context) error {
	callerID, _ := middleware.UserIDFromContext(c)
	role, _ := c.Get(middleware.CtxRole).(string)
	id, _ := uuid.Parse(c.Param("id"))
	oldPDF, err := h.svc.Delete(c.Request().Context(), id, callerID, role == "admin")
	if err != nil {
		switch err {
		case services.ErrArticleNotFound: return httpx.NotFound(c, "Article")
		case services.ErrNotOwner: return httpx.Forbidden(c, "You do not own this article")
		default: return httpx.Internal(c)
		}
	}
	if oldPDF != nil { go h.store.Delete(c.Request().Context(), storage.StorageKey(*oldPDF, h.store.PublicURL)) }
	return httpx.NoContent(c)
}

func (h *ArticleHandler) listMine(c echo.Context) error {
	userID, _ := middleware.UserIDFromContext(c)
	list, err := h.svc.ListByUser(c.Request().Context(), userID)
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, list)
}

// ══════════════════════════════════════════════════════════════
// RatingHandler
// ══════════════════════════════════════════════════════════════

type RatingHandler struct {
	rating *services.RatingService
	review *services.ReviewService
}

func NewRatingHandler(r *services.RatingService, rv *services.ReviewService) *RatingHandler {
	return &RatingHandler{r, rv}
}

func (h *RatingHandler) Register(g *echo.Group, authMW, optionalMW, rl echo.MiddlewareFunc) {
	for _, item := range []struct {
		path string
		kind models.ContentKind
	}{
		{"/drawings", models.KindDrawing},
		{"/articles", models.KindArticle},
	} {
		k := item
		g.POST(k.path+"/:id/rate", func(c echo.Context) error { return h.rate(c, k.kind) }, optionalMW, rl)
		g.GET(k.path+"/:id/reviews", func(c echo.Context) error { return h.listReviews(c, k.kind) }, optionalMW)
		g.POST(k.path+"/:id/reviews", func(c echo.Context) error { return h.createReview(c, k.kind) }, optionalMW, rl)
		g.POST(k.path+"/:id/reviews/:rid/reply", func(c echo.Context) error { return h.reply(c, k.kind) }, authMW)
	}
}

func (h *RatingHandler) rate(c echo.Context, kind models.ContentKind) error {
	contentID, _ := uuid.Parse(c.Param("id"))
	var req models.RateRequest
	if err := httpx.BindAndValidate(c, &req); err != nil { return err }
	userID, hasUser := middleware.UserIDFromContext(c)
	var uid *uuid.UUID
	if hasUser { uid = &userID }
	if err := h.rating.Rate(c.Request().Context(), kind, contentID, req.Score, uid, middleware.GuestIP(c)); err != nil {
		c.Logger().Error(err); return httpx.Internal(c)
	}
	return httpx.OK(c, map[string]string{"message": "Rating submitted"})
}

func (h *RatingHandler) listReviews(c echo.Context, kind models.ContentKind) error {
	contentID, _ := uuid.Parse(c.Param("id"))
	reviews, err := h.review.ListForContent(c.Request().Context(), kind, contentID)
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, reviews)
}

func (h *RatingHandler) createReview(c echo.Context, kind models.ContentKind) error {
	contentID, _ := uuid.Parse(c.Param("id"))
	var req models.CreateReviewRequest
	if err := httpx.BindAndValidate(c, &req); err != nil { return err }
	userID, hasUser := middleware.UserIDFromContext(c)
	var uid *uuid.UUID
	if hasUser { uid = &userID }
	rv, err := h.review.Create(c.Request().Context(), kind, contentID, req, uid, middleware.GuestIP(c))
	if err != nil { c.Logger().Error(err); return httpx.Internal(c) }
	return httpx.Created(c, rv)
}

func (h *RatingHandler) reply(c echo.Context, kind models.ContentKind) error {
	contentID, _ := uuid.Parse(c.Param("id"))
	reviewID, _ := uuid.Parse(c.Param("rid"))
	callerID, _ := middleware.UserIDFromContext(c)
	var req models.CreateReplyRequest
	if err := httpx.BindAndValidate(c, &req); err != nil { return err }
	rep, err := h.review.Reply(c.Request().Context(), reviewID, callerID, kind, contentID, req)
	if err != nil {
		if err == services.ErrNotOwner { return httpx.Forbidden(c, "Only the content owner can reply to reviews") }
		return httpx.Internal(c)
	}
	return httpx.Created(c, rep)
}

// ══════════════════════════════════════════════════════════════
// OrderHandler
// ══════════════════════════════════════════════════════════════

type OrderHandler struct {
	orderSvc  *services.OrderService
	emailSvc  *services.EmailService
	notifSvc  *services.NotificationService
}

func NewOrderHandler(o *services.OrderService, e *services.EmailService, n *services.NotificationService) *OrderHandler {
	return &OrderHandler{o, e, n}
}

func (h *OrderHandler) Register(g *echo.Group, optionalMW, authMW, adminMW echo.MiddlewareFunc) {
	g.POST("/orders", h.createOrder, optionalMW)
	g.GET("/admin/orders", h.listOrders, authMW, adminMW)
	g.GET("/admin/revenue/summary", h.revenueSummary, authMW, adminMW)
	g.GET("/admin/revenue/by-issue", h.revenueByIssue, authMW, adminMW)
	g.PUT("/admin/orders/:id/mark-paid", h.markPaid, authMW, adminMW)
}

func (h *OrderHandler) createOrder(c echo.Context) error {
	var req models.CreateOrderRequest
	if err := httpx.BindAndValidate(c, &req); err != nil { return err }
	userID, hasUser := middleware.UserIDFromContext(c)
	var uid *uuid.UUID
	if hasUser { uid = &userID }
	order, err := h.orderSvc.Create(c.Request().Context(), req, uid)
	if err != nil {
		if err == services.ErrIssueNotFound { return httpx.NotFound(c, "Issue") }
		c.Logger().Error(err); return httpx.Internal(c)
	}
	issueName := ""
	if order.IssueName != nil { issueName = *order.IssueName }
	go h.emailSvc.SendOrderConfirmation(order.BuyerEmail, order.BuyerName, issueName, string(order.Format), order.Total/100)
	if hasUser { go h.notifSvc.NotifyOrderConfirmed(c.Request().Context(), userID, issueName, order.ID) }
	return httpx.Created(c, order)
}

func (h *OrderHandler) listOrders(c echo.Context) error {
	var issueID *uuid.UUID
	if id := c.QueryParam("issue_id"); id != "" { if uid, err := uuid.Parse(id); err == nil { issueID = &uid } }
	var format *string
	if f := c.QueryParam("format"); f != "" { format = &f }
	orders, err := h.orderSvc.ListAdmin(c.Request().Context(), issueID, format)
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, orders)
}

func (h *OrderHandler) revenueSummary(c echo.Context) error {
	sum, err := h.orderSvc.RevenueSummary(c.Request().Context())
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, sum)
}

func (h *OrderHandler) revenueByIssue(c echo.Context) error {
	data, err := h.orderSvc.RevenueByIssue(c.Request().Context())
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, data)
}

func (h *OrderHandler) markPaid(c echo.Context) error {
	id, _ := uuid.Parse(c.Param("id"))
	if err := h.orderSvc.MarkPaid(c.Request().Context(), id); err != nil { return httpx.Internal(c) }
	return httpx.OK(c, map[string]string{"status": "paid"})
}

// ══════════════════════════════════════════════════════════════
// AdminHandler
// ══════════════════════════════════════════════════════════════

type AdminHandler struct{ svc *services.AdminService }

func NewAdminHandler(a *services.AdminService) *AdminHandler { return &AdminHandler{a} }

func (h *AdminHandler) Register(g *echo.Group, authMW, adminMW echo.MiddlewareFunc) {
	adm := g.Group("/admin", authMW, adminMW)
	adm.PUT("/drawings/:id/approve", h.approveDrawing)
	adm.PUT("/drawings/:id/reject", h.rejectDrawing)
	adm.PUT("/articles/:id/approve", h.approveArticle)
	adm.PUT("/drawings/:id/top30", h.setDrawingTop30)
	adm.PUT("/articles/:id/top30", h.setArticleTop30)
	adm.GET("/top3/suggestions/:issue_id", h.getTop3Suggestions)
	adm.POST("/top3/:issue_id/confirm", h.confirmTop3)
	adm.GET("/users", h.listUsers)
	adm.PUT("/users/:id/trusted", h.setTrusted)
	adm.PUT("/users/:id/active", h.setActive)
	adm.GET("/audit-log", h.listAuditLog)
	adm.GET("/contact-messages", h.listContactMessages)
	adm.POST("/contact-messages/:id/reply", h.replyContact)
	adm.GET("/content/:key", h.getContent)
	adm.PUT("/content/:key", h.setContent)
}

func (h *AdminHandler) approveDrawing(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	id, _ := uuid.Parse(c.Param("id"))
	if err := h.svc.ApproveDrawing(c.Request().Context(), adminID, id); err != nil {
		if err == services.ErrDrawingNotFound { return httpx.NotFound(c, "Drawing") }
		return httpx.Internal(c)
	}
	return httpx.OK(c, map[string]string{"status": "live"})
}

func (h *AdminHandler) rejectDrawing(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	id, _ := uuid.Parse(c.Param("id"))
	h.svc.RejectDrawing(c.Request().Context(), adminID, id)
	return httpx.OK(c, map[string]string{"status": "rejected"})
}

func (h *AdminHandler) approveArticle(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	id, _ := uuid.Parse(c.Param("id"))
	h.svc.ApproveArticle(c.Request().Context(), adminID, id)
	return httpx.OK(c, map[string]string{"status": "live"})
}

func (h *AdminHandler) setDrawingTop30(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	id, _ := uuid.Parse(c.Param("id"))
	var body struct{ Top30 bool `json:"top30"` }
	c.Bind(&body)
	h.svc.SetDrawingTop30(c.Request().Context(), adminID, id, body.Top30)
	return httpx.OK(c, map[string]bool{"top30": body.Top30})
}

func (h *AdminHandler) setArticleTop30(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	id, _ := uuid.Parse(c.Param("id"))
	var body struct{ Top30 bool `json:"top30"` }
	c.Bind(&body)
	h.svc.SetArticleTop30(c.Request().Context(), adminID, id, body.Top30)
	return httpx.OK(c, map[string]bool{"top30": body.Top30})
}

func (h *AdminHandler) getTop3Suggestions(c echo.Context) error {
	issueID, _ := uuid.Parse(c.Param("issue_id"))
	suggestions, err := h.svc.GetTop3Suggestions(c.Request().Context(), issueID)
	if err != nil { c.Logger().Error(err); return httpx.Internal(c) }
	return httpx.OK(c, suggestions)
}

func (h *AdminHandler) confirmTop3(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	issueID, _ := uuid.Parse(c.Param("issue_id"))
	var body struct {
		Selections []struct {
			UserID   uuid.UUID        `json:"user_id"`
			AgeGroup models.AgeGroup  `json:"age_group"`
			Rank     int16            `json:"rank"`
			Score    float64          `json:"score"`
		} `json:"selections"`
	}
	if err := c.Bind(&body); err != nil { return httpx.BadRequest(c, "Invalid body") }
	for _, sel := range body.Selections {
		_, err := h.svc.ConfirmTop3Entry(c.Request().Context(), adminID, issueID, sel.UserID, sel.AgeGroup, sel.Rank, sel.Score)
		if err != nil { c.Logger().Error(err); return httpx.Internal(c) }
	}
	return httpx.OK(c, map[string]string{"message": "Top 3 confirmed"})
}

func (h *AdminHandler) listUsers(c echo.Context) error {
	users, err := h.svc.ListUsers(c.Request().Context())
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, users)
}

func (h *AdminHandler) setTrusted(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	id, _ := uuid.Parse(c.Param("id"))
	var body struct{ Trusted bool `json:"trusted"` }
	c.Bind(&body)
	h.svc.SetTrusted(c.Request().Context(), adminID, id, body.Trusted)
	return httpx.OK(c, map[string]bool{"trusted": body.Trusted})
}

func (h *AdminHandler) setActive(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	id, _ := uuid.Parse(c.Param("id"))
	var body struct{ Active bool `json:"active"` }
	c.Bind(&body)
	h.svc.SetActive(c.Request().Context(), adminID, id, body.Active)
	return httpx.OK(c, map[string]bool{"active": body.Active})
}

func (h *AdminHandler) listAuditLog(c echo.Context) error {
	log, err := h.svc.ListAuditLog(c.Request().Context(), 100)
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, log)
}

func (h *AdminHandler) listContactMessages(c echo.Context) error {
	msgs, err := h.svc.ListContactMessages(c.Request().Context())
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, msgs)
}

func (h *AdminHandler) replyContact(c echo.Context) error {
	adminID, _ := middleware.UserIDFromContext(c)
	msgID, _ := uuid.Parse(c.Param("id"))
	var body struct{ ReplyBody string `json:"reply_body" validate:"required,max=2000"` }
	if err := httpx.BindAndValidate(c, &body); err != nil { return err }
	h.svc.ReplyContact(c.Request().Context(), adminID, msgID, body.ReplyBody)
	return httpx.OK(c, map[string]string{"status": "replied"})
}

func (h *AdminHandler) getContent(c echo.Context) error {
	key := c.Param("key"); lang := c.QueryParam("lang"); if lang == "" { lang = "en" }
	content, err := h.svc.GetContent(c.Request().Context(), key, lang)
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, content)
}

func (h *AdminHandler) setContent(c echo.Context) error {
	key := c.Param("key"); lang := c.QueryParam("lang"); if lang == "" { lang = "en" }
	var body map[string]interface{}
	if err := c.Bind(&body); err != nil { return httpx.BadRequest(c, "Invalid body") }
	h.svc.SetContent(c.Request().Context(), key, lang, body)
	return httpx.OK(c, body)
}

// ══════════════════════════════════════════════════════════════
// SearchHandler
// ══════════════════════════════════════════════════════════════

type SearchHandler struct{ svc *services.SearchService }

func NewSearchHandler(s *services.SearchService) *SearchHandler { return &SearchHandler{s} }

func (h *SearchHandler) Register(g *echo.Group) {
	g.GET("/search", h.search)
}

func (h *SearchHandler) search(c echo.Context) error {
	q := c.QueryParam("q")
	if len(q) < 2 { return httpx.BadRequest(c, "Search query must be at least 2 characters") }
	results, err := h.svc.Search(c.Request().Context(), q)
	if err != nil { c.Logger().Error(err); return httpx.Internal(c) }
	return httpx.OK(c, map[string]interface{}{"query": q, "results": results, "total": len(results)})
}

// ══════════════════════════════════════════════════════════════
// ContentHandler (public contact form + site content reads)
// ══════════════════════════════════════════════════════════════

type ContentHandler struct {
	adminSvc *services.AdminService
	rdb      *redis.Client
}

func NewContentHandler(a *services.AdminService, rdb *redis.Client) *ContentHandler {
	return &ContentHandler{a, rdb}
}

func (h *ContentHandler) Register(g *echo.Group) {
	g.GET("/content/:key", h.getContent)
	g.POST("/contact", h.submitContact, middleware.RateLimit(h.rdb, 3, time.Hour, "contact"))
}

func (h *ContentHandler) getContent(c echo.Context) error {
	key := c.Param("key"); lang := c.QueryParam("lang"); if lang == "" { lang = "en" }
	if key != "about" && key != "contact" && key != "guidelines" { return httpx.NotFound(c, "Content") }
	content, err := h.adminSvc.GetContent(c.Request().Context(), key, lang)
	if err != nil { return httpx.Internal(c) }
	return httpx.OK(c, content)
}

func (h *ContentHandler) submitContact(c echo.Context) error {
	var req struct {
		Name    string `json:"name" validate:"required,max=120"`
		Email   string `json:"email" validate:"required,email"`
		Message string `json:"message" validate:"required,max=2000"`
	}
	if err := httpx.BindAndValidate(c, &req); err != nil { return err }
	if err := h.adminSvc.CreateContactMessage(c.Request().Context(), req.Name, req.Email, req.Message); err != nil {
		c.Logger().Error(err); return httpx.Internal(c)
	}
	return httpx.OK(c, map[string]string{"message": "Your message has been received!"})
}
