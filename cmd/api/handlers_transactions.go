package main

import (
	"net/http"
	"strings"
	"time"
)

func (a *app) createPurchaseRequest(w http.ResponseWriter, r *http.Request, user User) {
	itemID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	var req struct {
		PaymentMethod string `json:"paymentMethod"`
	}
	if r.Body != http.NoBody {
		if !decode(w, r, &req) {
			return
		}
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	item, ok := a.store.items[itemID]
	if !ok || item.Status != "published" || item.SellerHidden {
		writeError(w, http.StatusNotFound, "published item not found")
		return
	}
	if item.SellerID == user.ID {
		writeError(w, http.StatusBadRequest, "seller cannot buy own item")
		return
	}
	status := "active"
	if req.PaymentMethod == "konbini" {
		status = "pending"
	} else if req.PaymentMethod != "" && req.PaymentMethod != "instant" {
		writeError(w, http.StatusBadRequest, "paymentMethod must be instant or konbini")
		return
	}
	item.Status = "sold"
	a.store.items[item.ID] = item
	if err := a.store.saveItem(item); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save item")
		return
	}
	item = a.store.enrichItem(item, user.ID)
	txn := Transaction{ID: a.store.nextTxnID, ItemID: item.ID, BuyerID: user.ID, SellerID: item.SellerID, Status: status, CreatedAt: time.Now(), Item: &item}
	a.store.nextTxnID++
	a.store.transactions[txn.ID] = txn
	if err := a.store.saveTransaction(txn); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save transaction")
		return
	}
	writeJSON(w, http.StatusCreated, txn)
}

func (a *app) listTransactions(w http.ResponseWriter, r *http.Request, user User) {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	var txns []Transaction
	for _, txn := range a.store.transactions {
		if txn.BuyerID == user.ID || txn.SellerID == user.ID {
			if txn.BuyerID == user.ID && txn.BuyerHidden {
				continue
			}
			if txn.SellerID == user.ID && txn.SellerHidden {
				continue
			}
			item := a.store.enrichItem(a.store.items[txn.ItemID], user.ID)
			txn.Item = &item
			txn = a.store.enrichTransaction(txn, user.ID)
			txns = append(txns, txn)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": txns})
}

func (a *app) deleteTransaction(w http.ResponseWriter, r *http.Request, user User) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid transaction id")
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	txn, ok := a.store.transactions[id]
	if !ok || (txn.BuyerID != user.ID && txn.SellerID != user.ID) {
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	if txn.Status != "done" {
		writeError(w, http.StatusBadRequest, "completed transaction can be deleted")
		return
	}
	myReviewed, partnerReviewed := a.store.reviewState(txn, user.ID)
	if !myReviewed || !partnerReviewed {
		writeError(w, http.StatusBadRequest, "transaction waiting for review cannot be deleted")
		return
	}
	if txn.BuyerID == user.ID {
		txn.BuyerHidden = true
	}
	if txn.SellerID == user.ID {
		txn.SellerHidden = true
	}
	a.store.transactions[id] = txn
	if err := a.store.saveTransaction(txn); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete transaction")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (a *app) approveTransaction(w http.ResponseWriter, r *http.Request, user User) {
	writeError(w, http.StatusGone, "seller approval is not used")
}

func (a *app) payTransaction(w http.ResponseWriter, r *http.Request, user User) {
	a.changeTransaction(w, r, user, "active")
}

func (a *app) completeTransaction(w http.ResponseWriter, r *http.Request, user User) {
	a.changeTransaction(w, r, user, "done")
}

func (a *app) changeTransaction(w http.ResponseWriter, r *http.Request, user User, status string) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid transaction id")
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	txn, ok := a.store.transactions[id]
	if !ok || (txn.BuyerID != user.ID && txn.SellerID != user.ID) {
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	if status == "active" && txn.BuyerID != user.ID {
		writeError(w, http.StatusForbidden, "only buyer can pay")
		return
	}
	if status == "active" && txn.Status != "pending" {
		writeError(w, http.StatusBadRequest, "pending transaction required")
		return
	}
	if status == "done" && txn.BuyerID != user.ID {
		writeError(w, http.StatusForbidden, "only buyer can complete")
		return
	}
	if status == "done" && txn.Status != "active" {
		writeError(w, http.StatusBadRequest, "active transaction required")
		return
	}
	txn.Status = status
	if status == "done" {
		now := time.Now()
		txn.CompletedAt = &now
	}
	a.store.transactions[id] = txn
	if err := a.store.saveTransaction(txn); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save transaction")
		return
	}
	writeJSON(w, http.StatusOK, txn)
}

func (a *app) listMessages(w http.ResponseWriter, r *http.Request, user User) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid transaction id")
		return
	}
	if !a.canAccessTransaction(id, user.ID) {
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	a.store.mu.RLock()
	msgs := a.store.messages[id]
	a.store.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (a *app) createMessage(w http.ResponseWriter, r *http.Request, user User) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid transaction id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, "message body is required")
		return
	}
	if !a.canAccessTransaction(id, user.ID) {
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	msg := Message{ID: a.store.nextMsgID, TransactionID: id, SenderID: user.ID, Body: req.Body, SentAt: time.Now()}
	a.store.nextMsgID++
	a.store.messages[id] = append(a.store.messages[id], msg)
	if err := a.store.saveMessage(msg); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save message")
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

func (a *app) createReview(w http.ResponseWriter, r *http.Request, user User) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid transaction id")
		return
	}
	var req struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	txn, ok := a.store.transactions[id]
	if !ok || txn.Status != "done" || (txn.BuyerID != user.ID && txn.SellerID != user.ID) {
		writeError(w, http.StatusBadRequest, "completed transaction required")
		return
	}
	if req.Rating < 1 || req.Rating > 5 {
		writeError(w, http.StatusBadRequest, "rating must be 1-5")
		return
	}
	if user.ID == txn.SellerID {
		_, buyerReviewed := a.store.reviewState(txn, user.ID)
		if !buyerReviewed {
			writeError(w, http.StatusBadRequest, "seller can review after buyer review")
			return
		}
	}
	for _, existing := range a.store.reviews {
		if existing.TransactionID == id && existing.ReviewerID == user.ID {
			writeError(w, http.StatusConflict, "review already submitted")
			return
		}
	}
	reviewee := txn.SellerID
	if user.ID == txn.SellerID {
		reviewee = txn.BuyerID
	}
	review := Review{ID: a.store.nextReviewID, TransactionID: id, ReviewerID: user.ID, RevieweeID: reviewee, Rating: req.Rating, Comment: req.Comment}
	a.store.nextReviewID++
	a.store.reviews[review.ID] = review
	if err := a.store.saveReview(review); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save review")
		return
	}
	if err := a.store.recalculateUserRating(reviewee); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update rating")
		return
	}
	writeJSON(w, http.StatusCreated, a.store.enrichReview(review))
}

func (a *app) listReviews(w http.ResponseWriter, r *http.Request, user User) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid transaction id")
		return
	}
	if !a.canAccessTransaction(id, user.ID) {
		writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	var reviews []Review
	for _, review := range a.store.reviews {
		if review.TransactionID == id && a.store.reviewVisibleToUser(review, user.ID) {
			reviews = append(reviews, a.store.enrichReview(review))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": reviews})
}
