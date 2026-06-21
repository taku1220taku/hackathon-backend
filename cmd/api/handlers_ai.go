package main

import (
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

func (a *app) listingAssist(w http.ResponseWriter, r *http.Request, user User) {
	var req ListingAssistRequest
	if !decode(w, r, &req) {
		return
	}
	result, err := a.ai.Assist(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	normalizeAssistCategory(&result)
	writeJSON(w, http.StatusOK, result)
}

func (a *app) priceSuggest(w http.ResponseWriter, r *http.Request, user User) {
	var req struct {
		Description    string   `json:"description"`
		CategoryID     int64    `json:"categoryId"`
		Category       string   `json:"category"`
		ConditionScore int      `json:"conditionScore"`
		Title          string   `json:"title"`
		TargetSellDays int      `json:"targetSellDays"`
		Images         []string `json:"images"`
	}
	if !decode(w, r, &req) {
		return
	}
	result, err := suggestPriceWithAI(r.Context(), req.Title, req.Description, req.CategoryID, req.Category, req.ConditionScore, req.TargetSellDays, req.Images)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *app) dynamicPrice(w http.ResponseWriter, r *http.Request, user User) {
	var req DynamicPriceRequest
	if !decode(w, r, &req) {
		return
	}
	if req.ItemID > 0 {
		a.store.mu.RLock()
		item, ok := a.store.items[req.ItemID]
		if ok && item.SellerID != user.ID {
			a.store.mu.RUnlock()
			writeError(w, http.StatusForbidden, "only seller can price this item")
			return
		}
		if ok {
			metrics := a.store.itemMetrics(item.ID)
			req.Title = item.Title
			req.CategoryID = item.CategoryID
			req.Category = item.Category
			req.CurrentPrice = item.Price
			req.ConditionScore = item.ConditionScore
			req.LikeCount = metrics.LikeCount
			req.ViewCount = metrics.ViewCount
			req.RecentViewCount = metrics.RecentViewCount
			req.ViewVelocity = metrics.ViewVelocity
		}
		a.store.mu.RUnlock()
		if !ok {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
	}
	if req.CurrentPrice <= 0 {
		writeError(w, http.StatusBadRequest, "currentPrice is required")
		return
	}
	if req.ConditionScore <= 0 {
		req.ConditionScore = 75
	}
	if req.TargetSellDays <= 0 {
		req.TargetSellDays = 7
	}
	if req.TargetSellDays > 60 {
		req.TargetSellDays = 60
	}
	if req.CategoryID == 0 {
		req.CategoryID = categoryIDByLabel(req.Category)
	}
	req.Category = categoryLabelByID(req.CategoryID)
	writeJSON(w, http.StatusOK, solveDynamicPrice(req))
}

func (a *app) fraudCheck(w http.ResponseWriter, r *http.Request, user User) {
	var req Item
	if !decode(w, r, &req) {
		return
	}
	normalizeItemCategory(&req)
	result, err := checkListingWithAI(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *app) itemQuestion(w http.ResponseWriter, r *http.Request, user User) {
	var req ItemQuestionRequest
	if !decode(w, r, &req) {
		return
	}
	if req.ItemID == 0 || strings.TrimSpace(req.Question) == "" {
		writeError(w, http.StatusBadRequest, "itemId and question are required")
		return
	}
	a.store.mu.RLock()
	item, ok := a.store.items[req.ItemID]
	a.store.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	answer, err := answerItemQuestion(r.Context(), item, req.Question)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ItemQuestionResult{Answer: answer})
}

func (a *app) geminiStatus(w http.ResponseWriter, r *http.Request, user User) {
	provider := geminiProvider()
	model := geminiModel()
	result := map[string]any{
		"configured": geminiConfigured(),
		"provider":   provider,
		"projectId":  vertexProjectID(),
		"location":   vertexLocation(),
		"model":      model,
		"live":       false,
	}
	if !geminiConfigured() || r.URL.Query().Get("live") != "1" {
		writeJSON(w, http.StatusOK, result)
		return
	}
	answer, err := callGemini(r.Context(), os.Getenv("GEMINI_API_KEY"), model, "CapCycle Gemini connection check. Reply with OK only.", false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"configured": true,
			"provider":   provider,
			"projectId":  vertexProjectID(),
			"location":   vertexLocation(),
			"model":      model,
			"live":       false,
			"error":      err.Error(),
		})
		return
	}
	result["live"] = true
	result["answer"] = strings.TrimSpace(answer)
	writeJSON(w, http.StatusOK, result)
}

func (a *app) recommendations(w http.ResponseWriter, r *http.Request, user User) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 12
	}
	a.store.mu.RLock()
	categoryWeight := map[string]int{}
	for _, txn := range a.store.transactions {
		if txn.BuyerID != user.ID && txn.SellerID != user.ID {
			continue
		}
		if item, ok := a.store.items[txn.ItemID]; ok {
			categoryWeight[item.Category] += 1
		}
	}
	var all []Item
	for _, item := range a.store.items {
		if item.Status == "published" && item.SellerID != user.ID {
			all = append(all, a.store.enrichItem(item, user.ID))
		}
	}
	a.store.mu.RUnlock()
	sort.SliceStable(all, func(i, j int) bool {
		left := categoryWeight[all[i].Category]*10000 + all[i].LikeCount*500 + all[i].RecentViewCount*100 + int(all[i].ViewVelocity*70) + all[i].ConditionScore*100 - all[i].Price/100
		right := categoryWeight[all[j].Category]*10000 + all[j].LikeCount*500 + all[j].RecentViewCount*100 + int(all[j].ViewVelocity*70) + all[j].ConditionScore*100 - all[j].Price/100
		return left > right
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
