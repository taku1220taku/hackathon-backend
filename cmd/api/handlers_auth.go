package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (a *app) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Email == "" || len(req.Password) < 8 || req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "email, displayName and password(8+) are required")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if _, ok := a.store.usersByEmail[req.Email]; ok {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}
	user := User{ID: a.store.nextUserID, Email: req.Email, PasswordHash: string(hash), DisplayName: req.DisplayName, Role: "user", Rating: 5, CreatedAt: time.Now()}
	a.store.nextUserID++
	a.store.users[user.ID] = user
	a.store.usersByEmail[user.Email] = user.ID
	if err := a.store.saveUser(user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save user")
		return
	}
	token := a.signToken(user)
	writeJSON(w, http.StatusCreated, map[string]any{"user": user, "token": token})
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.store.mu.RLock()
	id, ok := a.store.usersByEmail[req.Email]
	user := a.store.users[id]
	a.store.mu.RUnlock()
	if !ok || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "token": a.signToken(user)})
}

func (a *app) me(w http.ResponseWriter, r *http.Request, user User) {
	writeJSON(w, http.StatusOK, user)
}

func (a *app) updateMe(w http.ResponseWriter, r *http.Request, user User) {
	var req struct {
		DisplayName string `json:"displayName"`
		AvatarURL   string `json:"avatarUrl"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	user = a.store.users[user.ID]
	if req.DisplayName != "" {
		user.DisplayName = req.DisplayName
	}
	user.AvatarURL = req.AvatarURL
	a.store.users[user.ID] = user
	if err := a.store.saveUser(user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save profile")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (a *app) listMyItems(w http.ResponseWriter, r *http.Request, user User) {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	var items []Item
	for _, item := range a.store.items {
		if item.SellerID == user.ID && !item.SellerHidden {
			items = append(items, a.store.enrichItemForSeller(item))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *app) listMyReviews(w http.ResponseWriter, r *http.Request, user User) {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	var reviews []Review
	for _, review := range a.store.reviews {
		if review.RevieweeID == user.ID && a.store.reviewVisibleToUser(review, user.ID) {
			reviews = append(reviews, a.store.enrichReview(review))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": reviews})
}

func (a *app) adminStats(w http.ResponseWriter, r *http.Request, user User) {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	likeCount := 0
	for _, users := range a.store.itemLikes {
		likeCount += len(users)
	}
	viewCount := 0
	for _, views := range a.store.itemViews {
		viewCount += len(views)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users":        len(a.store.users),
		"items":        len(a.store.items),
		"transactions": len(a.store.transactions),
		"likes":        likeCount,
		"views":        viewCount,
	})
}

func (a *app) canAccessTransaction(txnID, userID int64) bool {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	txn, ok := a.store.transactions[txnID]
	return ok && (txn.BuyerID == userID || txn.SellerID == userID)
}

func (a *app) requireAuth(next func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims, err := a.verifyToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		a.store.mu.RLock()
		user, ok := a.store.users[claims.Sub]
		a.store.mu.RUnlock()
		if !ok {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}
		if claims.Role != "" && claims.Role != user.Role {
			writeError(w, http.StatusUnauthorized, "stale token")
			return
		}
		next(w, r, user)
	}
}

func (a *app) requireRole(role string, next func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request, user User) {
		if user.Role != role {
			writeError(w, http.StatusForbidden, "insufficient role")
			return
		}
		next(w, r, user)
	})
}

func (a *app) optionalUserID(r *http.Request) int64 {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	claims, err := a.verifyToken(token)
	if err != nil {
		return 0
	}
	a.store.mu.RLock()
	user, ok := a.store.users[claims.Sub]
	a.store.mu.RUnlock()
	if !ok || (claims.Role != "" && claims.Role != user.Role) {
		return 0
	}
	return claims.Sub
}

type tokenClaims struct {
	Sub  int64  `json:"sub"`
	Role string `json:"role"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

func (a *app) signToken(user User) string {
	header := b64(`{"alg":"HS256","typ":"JWT"}`)
	normalizeUserRole(&user)
	now := time.Now()
	payloadBytes, _ := json.Marshal(tokenClaims{
		Sub:  user.ID,
		Role: user.Role,
		Iat:  now.Unix(),
		Exp:  now.Add(24 * time.Hour).Unix(),
	})
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	body := header + "." + payload
	mac := hmac.New(sha256.New, a.jwtSecret)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *app) verifyToken(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("invalid token")
	}
	body := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, a.jwtSecret)
	mac.Write([]byte(body))
	if !hmac.Equal([]byte(parts[2]), []byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))) {
		return tokenClaims{}, errors.New("invalid signature")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, err
	}
	var payload tokenClaims
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return tokenClaims{}, err
	}
	if payload.Sub <= 0 || payload.Exp <= 0 {
		return tokenClaims{}, errors.New("invalid claims")
	}
	if time.Now().Unix() > payload.Exp {
		return tokenClaims{}, errors.New("expired token")
	}
	if payload.Role != "" && payload.Role != "user" && payload.Role != "admin" {
		return tokenClaims{}, errors.New("invalid role")
	}
	return payload, nil
}
