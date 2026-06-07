package api

import (
	"crypto/rand"
	"math/big"
	"net/http"

	"github.com/free-model-gateway/fmg/internal/middleware"
	"github.com/free-model-gateway/fmg/internal/store"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	store *store.Store
}

func NewAuthHandler(st *store.Store) *AuthHandler {
	return &AuthHandler{store: st}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Captcha  string `json:"captcha"`
}

func (a *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
		return
	}

	sessionCaptcha, _ := c.Cookie("captcha")
	if sessionCaptcha == "" || req.Captcha == "" || sessionCaptcha != req.Captcha {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid captcha"})
		return
	}

	user, err := a.store.VerifyPassword(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := middleware.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.SetCookie("auth_token", token, 7*24*60*60, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"nickname": user.Nickname,
			"avatar":   user.Avatar,
			"role":     user.Role,
		},
	})
}

func (a *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("auth_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (a *AuthHandler) Me(c *gin.Context) {
	userID, _ := c.Get("user_id")
	u, err := a.store.GetUserByID(c.Request.Context(), userID.(int64))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if u == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":       u.ID,
		"username": u.Username,
		"nickname": u.Nickname,
		"avatar":   u.Avatar,
		"role":     u.Role,
		"api_key":  u.APIKey,
	})
}

func (a *AuthHandler) RefreshToken(c *gin.Context) {
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")
	role, _ := c.Get("role")

	user := &store.User{
		ID:       userID.(int64),
		Username: username.(string),
		Role:     role.(string),
	}

	token, err := middleware.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.SetCookie("auth_token", token, 7*24*60*60, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"token": token})
}

func (a *AuthHandler) Captcha(c *gin.Context) {
	captcha := generateCaptcha()
	c.SetCookie("captcha", captcha, 300, "/", "", false, false)
	c.JSON(http.StatusOK, gin.H{"captcha": captcha})
}

func generateCaptcha() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 4)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[n.Int64()]
	}
	return string(b)
}
