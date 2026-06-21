package main

import (
	"math"
	"time"
)

func newStore() *store {
	return &store{
		nextUserID:   1,
		nextItemID:   1,
		nextTxnID:    1,
		nextMsgID:    1,
		nextReviewID: 1,
		users:        map[int64]User{},
		usersByEmail: map[string]int64{},
		items:        map[int64]Item{},
		itemLikes:    map[int64]map[int64]bool{},
		itemViews:    map[int64][]ItemView{},
		transactions: map[int64]Transaction{},
		messages:     map[int64][]Message{},
		reviews:      map[int64]Review{},
	}
}

func normalizeUserRole(user *User) {
	if user.Role != "admin" {
		user.Role = "user"
	}
}

func (s *store) recalculateUserRating(userID int64) error {
	user, ok := s.users[userID]
	if !ok {
		return nil
	}
	total := 0
	count := 0
	for _, review := range s.reviews {
		if review.RevieweeID == userID {
			total += review.Rating
			count++
		}
	}
	if count == 0 {
		user.Rating = 5
	} else {
		user.Rating = float64(total) / float64(count)
	}
	s.users[userID] = user
	return s.saveUser(user)
}

func (s *store) reviewState(txn Transaction, userID int64) (bool, bool) {
	partnerID := txn.SellerID
	if userID == txn.SellerID {
		partnerID = txn.BuyerID
	}
	myReviewed := false
	partnerReviewed := false
	for _, review := range s.reviews {
		if review.TransactionID != txn.ID {
			continue
		}
		if review.ReviewerID == userID {
			myReviewed = true
		}
		if review.ReviewerID == partnerID {
			partnerReviewed = true
		}
	}
	return myReviewed, partnerReviewed
}

func (s *store) enrichTransaction(txn Transaction, userID int64) Transaction {
	myReviewed, partnerReviewed := s.reviewState(txn, userID)
	txn.MyReviewed = myReviewed
	txn.PartnerReviewed = partnerReviewed
	return txn
}

func (s *store) enrichReview(review Review) Review {
	if reviewer, ok := s.users[review.ReviewerID]; ok {
		review.ReviewerName = reviewer.DisplayName
	}
	if reviewee, ok := s.users[review.RevieweeID]; ok {
		review.RevieweeName = reviewee.DisplayName
	}
	if txn, ok := s.transactions[review.TransactionID]; ok {
		if review.ReviewerID == txn.BuyerID {
			review.ReviewerRole = "購入者"
		} else if review.ReviewerID == txn.SellerID {
			review.ReviewerRole = "出品者"
		}
		if review.RevieweeID == txn.BuyerID {
			review.RevieweeRole = "購入者"
		} else if review.RevieweeID == txn.SellerID {
			review.RevieweeRole = "出品者"
		}
	}
	return review
}

func (s *store) reviewVisibleToUser(review Review, userID int64) bool {
	txn, ok := s.transactions[review.TransactionID]
	if !ok {
		return true
	}
	if review.ReviewerID == userID {
		return true
	}
	if userID == txn.SellerID {
		sellerReviewed, _ := s.reviewState(txn, userID)
		return sellerReviewed
	}
	return true
}

func (s *store) sellerCanDeleteItem(item Item) bool {
	if s.itemHasIncompleteTransaction(item.ID, item.SellerID) {
		return false
	}
	if item.Status != "sold" {
		return true
	}
	for _, txn := range s.transactions {
		if txn.ItemID != item.ID || txn.SellerID != item.SellerID || txn.Status != "done" {
			continue
		}
		sellerReviewed, buyerReviewed := s.reviewState(txn, item.SellerID)
		return sellerReviewed && buyerReviewed
	}
	return false
}

func (s *store) likeCount(itemID int64) int {
	return len(s.itemLikes[itemID])
}

func (s *store) userLikesItem(userID, itemID int64) bool {
	if userID == 0 {
		return false
	}
	return s.itemLikes[itemID][userID]
}

func (s *store) addLikeInMemory(userID, itemID int64) {
	if s.itemLikes[itemID] == nil {
		s.itemLikes[itemID] = map[int64]bool{}
	}
	s.itemLikes[itemID][userID] = true
}

func (s *store) removeLikeInMemory(userID, itemID int64) {
	if s.itemLikes[itemID] == nil {
		return
	}
	delete(s.itemLikes[itemID], userID)
	if len(s.itemLikes[itemID]) == 0 {
		delete(s.itemLikes, itemID)
	}
}

func (s *store) itemMetrics(itemID int64) ItemMetrics {
	now := time.Now()
	recentSince := now.Add(-24 * time.Hour)
	velocitySince := now.Add(-72 * time.Hour)
	metrics := ItemMetrics{LikeCount: s.likeCount(itemID)}
	for _, view := range s.itemViews[itemID] {
		metrics.ViewCount++
		if view.CreatedAt.After(recentSince) {
			metrics.RecentViewCount++
		}
		if view.CreatedAt.After(velocitySince) {
			metrics.ViewVelocity += 1.0 / 3.0
		}
	}
	metrics.ViewVelocity = math.Round(metrics.ViewVelocity*10) / 10
	return metrics
}

func (s *store) shouldRecordView(itemID int64, viewerHash string, now time.Time) bool {
	for i := len(s.itemViews[itemID]) - 1; i >= 0; i-- {
		view := s.itemViews[itemID][i]
		if now.Sub(view.CreatedAt) > 30*time.Minute {
			return true
		}
		if view.ViewerHash == viewerHash {
			return false
		}
	}
	return true
}

func (s *store) addViewInMemory(view ItemView) {
	s.itemViews[view.ItemID] = append(s.itemViews[view.ItemID], view)
}

func (s *store) enrichItem(item Item, userID int64) Item {
	item.LikeCount = s.likeCount(item.ID)
	item.LikedByMe = s.userLikesItem(userID, item.ID)
	metrics := s.itemMetrics(item.ID)
	item.ViewCount = metrics.ViewCount
	item.RecentViewCount = metrics.RecentViewCount
	item.ViewVelocity = metrics.ViewVelocity
	return item
}

func (s *store) itemHasIncompleteTransaction(itemID, sellerID int64) bool {
	for _, txn := range s.transactions {
		if txn.ItemID == itemID && txn.SellerID == sellerID && txn.Status != "done" {
			return true
		}
	}
	return false
}

func (s *store) enrichItemForSeller(item Item) Item {
	item.SellerCanDelete = s.sellerCanDeleteItem(item)
	return s.enrichItem(item, item.SellerID)
}
