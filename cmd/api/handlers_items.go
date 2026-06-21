package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *app) listItems(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(r.URL.Query().Get("q"))
	category := strings.ToLower(r.URL.Query().Get("category"))
	categoryID, _ := strconv.ParseInt(r.URL.Query().Get("categoryId"), 10, 64)
	status := r.URL.Query().Get("status")
	sortMode := r.URL.Query().Get("sort")
	if status == "" {
		status = "published"
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	minPrice, _ := strconv.Atoi(r.URL.Query().Get("minPrice"))
	maxPrice, _ := strconv.Atoi(r.URL.Query().Get("maxPrice"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 12
	}
	viewerID := a.optionalUserID(r)
	a.store.mu.RLock()
	var all []Item
	for _, item := range a.store.items {
		if item.SellerHidden {
			continue
		}
		if status != "all" && item.Status != status {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.Description+" "+item.Context), query) {
			continue
		}
		if category != "" && strings.ToLower(item.Category) != category {
			continue
		}
		if categoryID > 0 && item.CategoryID != categoryID {
			continue
		}
		if minPrice > 0 && item.Price < minPrice {
			continue
		}
		if maxPrice > 0 && item.Price > maxPrice {
			continue
		}
		all = append(all, a.store.enrichItem(item, viewerID))
	}
	a.store.mu.RUnlock()
	sort.SliceStable(all, func(i, j int) bool {
		switch sortMode {
		case "popular":
			left := all[i].LikeCount*1000 + all[i].RecentViewCount*120 + int(all[i].ViewVelocity*80) + all[i].ConditionScore
			right := all[j].LikeCount*1000 + all[j].RecentViewCount*120 + int(all[j].ViewVelocity*80) + all[j].ConditionScore
			return left > right
		case "recommended":
			left := all[i].LikeCount*500 + all[i].RecentViewCount*100 + int(all[i].ViewVelocity*70) + all[i].ConditionScore*100 - all[i].Price/100
			right := all[j].LikeCount*500 + all[j].RecentViewCount*100 + int(all[j].ViewVelocity*70) + all[j].ConditionScore*100 - all[j].Price/100
			return left > right
		default:
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
	})
	start := (page - 1) * limit
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": all[start:end], "page": page, "hasMore": end < len(all)})
}

func (a *app) getItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	viewerID := a.optionalUserID(r)
	a.store.mu.RLock()
	item, ok := a.store.items[id]
	a.store.mu.RUnlock()
	if !ok || item.SellerHidden || item.Status == "draft" {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if viewerID != item.SellerID {
		if err := a.recordItemView(r, item.ID, viewerID); err != nil {
			log.Printf("failed to record item view: %v", err)
		}
	}
	a.store.mu.RLock()
	item = a.store.enrichItem(a.store.items[id], viewerID)
	a.store.mu.RUnlock()
	writeJSON(w, http.StatusOK, item)
}

func (a *app) itemMetrics(w http.ResponseWriter, r *http.Request, user User) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	a.store.mu.RLock()
	item, ok := a.store.items[id]
	if ok && item.SellerID != user.ID {
		a.store.mu.RUnlock()
		writeError(w, http.StatusForbidden, "only seller can view item metrics")
		return
	}
	metrics := a.store.itemMetrics(id)
	a.store.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (a *app) recordItemView(r *http.Request, itemID, viewerID int64) error {
	now := time.Now()
	viewerHash := a.viewerHash(r, viewerID)
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if !a.store.shouldRecordView(itemID, viewerHash, now) {
		return nil
	}
	view := ItemView{ItemID: itemID, ViewerID: viewerID, ViewerHash: viewerHash, CreatedAt: now}
	a.store.addViewInMemory(view)
	if err := a.store.saveItemView(view); err != nil {
		views := a.store.itemViews[itemID]
		if len(views) > 0 {
			a.store.itemViews[itemID] = views[:len(views)-1]
		}
		return err
	}
	return nil
}

func (a *app) viewerHash(r *http.Request, viewerID int64) string {
	source := fmt.Sprintf("user:%d", viewerID)
	if viewerID == 0 {
		ip := r.Header.Get("X-Forwarded-For")
		if comma := strings.Index(ip, ","); comma >= 0 {
			ip = strings.TrimSpace(ip[:comma])
		}
		if ip == "" {
			ip = r.Header.Get("X-Real-IP")
		}
		if ip == "" {
			ip = r.RemoteAddr
		}
		source = "anon:" + ip + ":" + r.UserAgent()
	}
	mac := hmac.New(sha256.New, a.jwtSecret)
	mac.Write([]byte(source))
	return fmt.Sprintf("%x", mac.Sum(nil))
}

func (a *app) createItem(w http.ResponseWriter, r *http.Request, user User) {
	var req Item
	if !decode(w, r, &req) {
		return
	}
	normalizeItemCategory(&req)
	if req.Title == "" || req.Price <= 0 {
		writeError(w, http.StatusBadRequest, "title and price are required")
		return
	}
	if req.Status == "" {
		req.Status = "draft"
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	req.ID = a.store.nextItemID
	a.store.nextItemID++
	req.SellerID = user.ID
	req.SellerHidden = false
	req.CreatedAt = time.Now()
	a.store.items[req.ID] = req
	if err := a.store.saveItem(req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save item")
		return
	}
	writeJSON(w, http.StatusCreated, a.store.enrichItemForSeller(req))
}

func (a *app) updateItem(w http.ResponseWriter, r *http.Request, user User) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	var req Item
	if !decode(w, r, &req) {
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	item, ok := a.store.items[id]
	if !ok {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if item.SellerID != user.ID {
		writeError(w, http.StatusForbidden, "only seller can update item")
		return
	}
	if req.Title != "" {
		item.Title = req.Title
	}
	if req.Description != "" {
		item.Description = req.Description
	}
	if req.Price > 0 {
		item.Price = req.Price
	}
	if req.Category != "" {
		item.Category = req.Category
	}
	if req.CategoryID > 0 {
		item.CategoryID = req.CategoryID
	}
	normalizeItemCategory(&item)
	if req.Status != "" {
		if req.Status != "published" && req.Status != item.Status && a.store.itemHasIncompleteTransaction(item.ID, user.ID) {
			writeError(w, http.StatusBadRequest, "item with active transaction cannot be unpublished")
			return
		}
		item.Status = req.Status
		if req.Status == "published" {
			item.SellerHidden = false
		}
	}
	if req.ConditionScore > 0 {
		item.ConditionScore = req.ConditionScore
	}
	if len(req.Images) > 0 {
		item.Images = req.Images
	}
	item.Context = req.Context
	item.ShippingFee = req.ShippingFee
	a.store.items[id] = item
	if err := a.store.saveItem(item); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save item")
		return
	}
	writeJSON(w, http.StatusOK, a.store.enrichItemForSeller(item))
}

func (a *app) deleteItem(w http.ResponseWriter, r *http.Request, user User) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	item, ok := a.store.items[id]
	if !ok || item.SellerID != user.ID {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if a.store.itemHasIncompleteTransaction(item.ID, user.ID) {
		writeError(w, http.StatusBadRequest, "item with active transaction cannot be deleted")
		return
	}
	if item.Status == "sold" {
		var soldTxn *Transaction
		for _, txn := range a.store.transactions {
			if txn.ItemID == item.ID && txn.SellerID == user.ID {
				nextTxn := txn
				soldTxn = &nextTxn
				break
			}
		}
		if soldTxn == nil || soldTxn.Status != "done" {
			writeError(w, http.StatusBadRequest, "sold item transaction is not complete")
			return
		}
		sellerReviewed, buyerReviewed := a.store.reviewState(*soldTxn, user.ID)
		if !sellerReviewed || !buyerReviewed {
			writeError(w, http.StatusBadRequest, "sold item waiting for review cannot be deleted")
			return
		}
		soldTxn.SellerHidden = true
		a.store.transactions[soldTxn.ID] = *soldTxn
		if err := a.store.saveTransaction(*soldTxn); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete transaction")
			return
		}
	}
	item.SellerHidden = true
	if item.Status == "published" {
		item.Status = "draft"
	}
	a.store.items[id] = item
	if err := a.store.saveItem(item); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete item")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (a *app) likeItem(w http.ResponseWriter, r *http.Request, user User) {
	a.changeItemLike(w, r, user, true)
}

func (a *app) unlikeItem(w http.ResponseWriter, r *http.Request, user User) {
	a.changeItemLike(w, r, user, false)
}

func (a *app) changeItemLike(w http.ResponseWriter, r *http.Request, user User, liked bool) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	item, ok := a.store.items[id]
	if !ok || item.SellerHidden || item.Status == "draft" {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	if liked {
		a.store.addLikeInMemory(user.ID, id)
		if err := a.store.saveLike(user.ID, id); err != nil {
			a.store.removeLikeInMemory(user.ID, id)
			writeError(w, http.StatusInternalServerError, "failed to like item")
			return
		}
	} else {
		wasLiked := a.store.userLikesItem(user.ID, id)
		a.store.removeLikeInMemory(user.ID, id)
		if err := a.store.deleteLike(user.ID, id); err != nil {
			if wasLiked {
				a.store.addLikeInMemory(user.ID, id)
			}
			writeError(w, http.StatusInternalServerError, "failed to unlike item")
			return
		}
	}
	writeJSON(w, http.StatusOK, a.store.enrichItem(item, user.ID))
}
