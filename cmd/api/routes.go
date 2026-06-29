package main

import (
	"context"
	"net/http"
	"time"
)

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		persistent := a.store.db != nil
		storage := "memory"
		if persistent {
			storage = "mysql"
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "storage": storage, "persistent": persistent})
	}
	readyHandler := func(w http.ResponseWriter, r *http.Request) {
		if a.store.db == nil {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "storage": "memory", "persistent": false})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := a.store.db.PingContext(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "storage": "mysql", "persistent": true})
	}
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readyHandler)
	mux.HandleFunc("POST /auth/register", a.register)
	mux.HandleFunc("POST /auth/login", a.login)
	mux.HandleFunc("GET /me", a.requireAuth(a.me))
	mux.HandleFunc("PATCH /me", a.requireAuth(a.updateMe))
	mux.HandleFunc("GET /me/items", a.requireAuth(a.listMyItems))
	mux.HandleFunc("GET /me/items/{id}", a.requireAuth(a.getMyItem))
	mux.HandleFunc("GET /me/reviews", a.requireAuth(a.listMyReviews))
	mux.HandleFunc("GET /admin/stats", a.requireRole("admin", a.adminStats))
	mux.HandleFunc("POST /uploads", a.requireAuth(a.uploadImage))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(env("UPLOAD_DIR", "uploads")))))
	mux.HandleFunc("GET /items", a.listItems)
	mux.HandleFunc("POST /items", a.requireAuth(a.createItem))
	mux.HandleFunc("GET /items/{id}", a.getItem)
	mux.HandleFunc("GET /items/{id}/metrics", a.requireAuth(a.itemMetrics))
	mux.HandleFunc("PATCH /items/{id}", a.requireAuth(a.updateItem))
	mux.HandleFunc("DELETE /items/{id}", a.requireAuth(a.deleteItem))
	mux.HandleFunc("POST /items/{id}/like", a.requireAuth(a.likeItem))
	mux.HandleFunc("DELETE /items/{id}/like", a.requireAuth(a.unlikeItem))
	mux.HandleFunc("POST /items/{id}/purchase-requests", a.requireAuth(a.createPurchaseRequest))
	mux.HandleFunc("GET /transactions", a.requireAuth(a.listTransactions))
	mux.HandleFunc("DELETE /transactions/{id}", a.requireAuth(a.deleteTransaction))
	mux.HandleFunc("POST /transactions/{id}/approve", a.requireAuth(a.approveTransaction))
	mux.HandleFunc("POST /transactions/{id}/pay", a.requireAuth(a.payTransaction))
	mux.HandleFunc("POST /transactions/{id}/complete", a.requireAuth(a.completeTransaction))
	mux.HandleFunc("GET /transactions/{id}/messages", a.requireAuth(a.listMessages))
	mux.HandleFunc("POST /transactions/{id}/messages", a.requireAuth(a.createMessage))
	mux.HandleFunc("GET /transactions/{id}/reviews", a.requireAuth(a.listReviews))
	mux.HandleFunc("POST /transactions/{id}/reviews", a.requireAuth(a.createReview))
	mux.HandleFunc("POST /ai/listing-assist", a.requireAuth(a.listingAssist))
	mux.HandleFunc("POST /ai/price-suggest", a.requireAuth(a.priceSuggest))
	mux.HandleFunc("POST /ai/dynamic-price", a.requireAuth(a.dynamicPrice))
	mux.HandleFunc("POST /ai/fraud-check", a.requireAuth(a.fraudCheck))
	mux.HandleFunc("POST /ai/item-question", a.requireAuth(a.itemQuestion))
	mux.HandleFunc("GET /ai/gemini-status", a.requireAuth(a.geminiStatus))
	mux.HandleFunc("GET /ai/recommendations", a.requireAuth(a.recommendations))
	return mux
}
