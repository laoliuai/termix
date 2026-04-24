package controlapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"
	openapi "github.com/termix/termix/go/gen/openapi"
	"github.com/termix/termix/go/internal/auth"
	"github.com/termix/termix/go/internal/control"
	"github.com/termix/termix/go/internal/persistence"
)

const accessTokenTTL = 15 * time.Minute
const controlLeaseTTL = 30 * time.Second

type server struct {
	store        *persistence.Store
	signingKey   string
	leaseService *control.LeaseService
}

func NewRouter(store *persistence.Store, signingKey string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if err := store.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	srv := &server{
		store:      store,
		signingKey: signingKey,
		leaseService: control.NewLeaseService(store, control.LeaseServiceConfig{
			TTL: controlLeaseTTL,
			Now: time.Now,
		}),
	}
	bearer := auth.BearerMiddleware(signingKey)

	openapi.RegisterHandlersWithOptions(router, srv, openapi.GinServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []openapi.MiddlewareFunc{
			func(c *gin.Context) {
				if _, requiresBearer := c.Get(openapi.BearerAuthScopes); !requiresBearer {
					return
				}
				bearer(c)
			},
		},
	})

	return router
}

func (s *server) PostAuthLogin(c *gin.Context) {
	var req openapi.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := s.store.GetUserByEmail(c.Request.Context(), string(req.Email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if auth.ComparePassword(user.PasswordHash, req.Password) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	device, err := s.store.CreateHostDevice(c.Request.Context(), user.ID, string(req.Platform), req.DeviceLabel, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := s.store.UpdateUserLastLogin(c.Request.Context(), user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	accessToken, err := auth.IssueAccessToken(s.signingKey, user.ID, device.ID, accessTokenTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	refreshToken, err := issueRefreshToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	userID, err := parseOpenAPIUUID(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	deviceID, err := parseOpenAPIUUID(device.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, openapi.LoginResponse{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		ExpiresInSeconds: int(accessTokenTTL.Seconds()),
		User: openapi.User{
			Id:          userID,
			Email:       openapi_types.Email(user.Email),
			DisplayName: user.DisplayName,
			Role:        openapi.UserRole(user.Role),
		},
		Device: openapi.Device{
			Id:         deviceID,
			DeviceType: openapi.DeviceDeviceType(device.DeviceType),
			Platform:   openapi.DevicePlatform(device.Platform),
			Label:      device.Label,
		},
	})
}

func (s *server) PostHostSessions(c *gin.Context) {
	var req openapi.CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := c.GetString("user_id")
	deviceID := c.GetString("device_id")
	if userID == "" || deviceID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing bearer claims"})
		return
	}
	if req.DeviceId.String() != deviceID {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "device mismatch"})
		return
	}

	name := ""
	if req.Name != nil {
		name = *req.Name
	}

	session, err := s.store.CreateSession(c.Request.Context(), persistence.CreateSessionParams{
		UserID:          userID,
		HostDeviceID:    req.DeviceId.String(),
		Name:            name,
		Tool:            string(req.Tool),
		LaunchCommand:   req.LaunchCommand,
		Cwd:             req.Cwd,
		CwdLabel:        req.CwdLabel,
		TmuxSessionName: "termix_" + uuid.NewString(),
		Status:          "starting",
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sessionID, err := parseOpenAPIUUID(session.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, openapi.CreateSessionResponse{
		SessionId:       sessionID,
		Status:          session.Status,
		TmuxSessionName: session.TmuxSessionName,
	})
}

func (s *server) PatchHostSession(c *gin.Context, sessionID openapi_types.UUID) {
	var req openapi.UpdateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	session, err := s.store.UpdateSessionStatus(c.Request.Context(), sessionID.String(), string(req.Status), req.LastError, req.LastExitCode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response, err := toOpenAPISession(session)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, response)
}

func (s *server) GetSession(c *gin.Context, sessionID openapi_types.UUID) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing bearer claims"})
		return
	}

	session, err := s.store.GetSessionForUser(c.Request.Context(), sessionID.String(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response, err := toOpenAPISession(session)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, response)
}

func (s *server) PostSessionControlAcquire(c *gin.Context, sessionID openapi_types.UUID) {
	lease, err := s.leaseService.Acquire(c.Request.Context(), controlActor(c), sessionID.String())
	if err != nil {
		writeLeaseError(c, err)
		return
	}

	resp, err := writeLease(c, lease)
	if err != nil {
		writeLeaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *server) PostSessionControlRenew(c *gin.Context, sessionID openapi_types.UUID) {
	leaseVersion, ok := bindLeaseVersion(c)
	if !ok {
		return
	}

	lease, err := s.leaseService.Renew(c.Request.Context(), controlActor(c), sessionID.String(), leaseVersion)
	if err != nil {
		writeLeaseError(c, err)
		return
	}

	resp, err := writeLease(c, lease)
	if err != nil {
		writeLeaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *server) PostSessionControlRelease(c *gin.Context, sessionID openapi_types.UUID) {
	leaseVersion, ok := bindLeaseVersion(c)
	if !ok {
		return
	}

	lease, err := s.leaseService.Release(c.Request.Context(), controlActor(c), sessionID.String(), leaseVersion)
	if err != nil {
		writeLeaseError(c, err)
		return
	}

	sessionUUID, err := parseOpenAPIUUID(lease.SessionID)
	if err != nil {
		writeLeaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, openapi.ReleaseControlLeaseResponse{
		SessionId:    sessionUUID,
		LeaseVersion: lease.LeaseVersion,
		Released:     true,
	})
}

func toOpenAPISession(session persistence.Session) (openapi.Session, error) {
	id, err := parseOpenAPIUUID(session.ID)
	if err != nil {
		return openapi.Session{}, err
	}
	userID, err := parseOpenAPIUUID(session.UserID)
	if err != nil {
		return openapi.Session{}, err
	}
	hostDeviceID, err := parseOpenAPIUUID(session.HostDeviceID)
	if err != nil {
		return openapi.Session{}, err
	}

	return openapi.Session{
		Id:              id,
		UserId:          userID,
		HostDeviceId:    hostDeviceID,
		Name:            session.Name,
		Tool:            openapi.SessionTool(session.Tool),
		LaunchCommand:   session.LaunchCommand,
		Cwd:             session.Cwd,
		CwdLabel:        session.CwdLabel,
		TmuxSessionName: session.TmuxSessionName,
		Status:          session.Status,
	}, nil
}

func controlActor(c *gin.Context) control.ControlActor {
	return control.ControlActor{
		UserID:   c.GetString("user_id"),
		DeviceID: c.GetString("device_id"),
	}
}

func bindLeaseVersion(c *gin.Context) (int64, bool) {
	var req struct {
		LeaseVersion *int64 `json:"lease_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "reason": "invalid_request"})
		return 0, false
	}
	if req.LeaseVersion == nil || *req.LeaseVersion <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lease_version must be a positive integer", "reason": "invalid_request"})
		return 0, false
	}
	return *req.LeaseVersion, true
}

func writeLease(c *gin.Context, lease persistence.ControlLease) (openapi.ControlLeaseResponse, error) {
	_ = c

	sessionID, err := parseOpenAPIUUID(lease.SessionID)
	if err != nil {
		return openapi.ControlLeaseResponse{}, err
	}
	controllerDeviceID, err := parseOpenAPIUUID(lease.ControllerDeviceID)
	if err != nil {
		return openapi.ControlLeaseResponse{}, err
	}

	return openapi.ControlLeaseResponse{
		SessionId:          sessionID,
		ControllerDeviceId: controllerDeviceID,
		LeaseVersion:       lease.LeaseVersion,
		GrantedAt:          lease.GrantedAt,
		ExpiresAt:          lease.ExpiresAt,
		RenewAfterSeconds:  control.RenewAfterSeconds(controlLeaseTTL),
	}, nil
}

func writeLeaseError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	reason := "internal"

	switch {
	case errors.Is(err, control.ErrUnauthorized):
		status = http.StatusUnauthorized
		reason = "unauthorized"
	case errors.Is(err, control.ErrNotFound):
		status = http.StatusNotFound
		reason = "not_found"
	case errors.Is(err, control.ErrSessionNotControllable):
		status = http.StatusConflict
		reason = "session_not_controllable"
	case errors.Is(err, control.ErrAlreadyControlled):
		status = http.StatusConflict
		reason = "already_controlled"
	case errors.Is(err, control.ErrStaleLease):
		status = http.StatusConflict
		reason = "stale_lease"
	}

	c.JSON(status, gin.H{"error": err.Error(), "reason": reason})
}

func parseOpenAPIUUID(raw string) (openapi_types.UUID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return openapi_types.UUID{}, err
	}
	return openapi_types.UUID(parsed), nil
}

func issueRefreshToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	if token == "" {
		return "", errors.New("failed to generate refresh token")
	}
	return token, nil
}
