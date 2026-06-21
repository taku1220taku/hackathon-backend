package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func openDatabase() (*sql.DB, error) {
	dsn := env("DATABASE_URL", "")
	if dsn == "" {
		dsn = mysqlDSN()
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func mysqlDSN() string {
	user := env("DB_USER", "capcycle")
	password := env("DB_PASSWORD", "capcycle")
	name := env("DB_NAME", "capcycle")
	if instance := env("CLOUD_SQL_CONNECTION_NAME", ""); instance != "" {
		return fmt.Sprintf("%s:%s@unix(/cloudsql/%s)/%s?parseTime=true&multiStatements=true&charset=utf8mb4,utf8", user, password, instance, name)
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true&charset=utf8mb4,utf8",
		user,
		password,
		env("DB_HOST", "127.0.0.1"),
		env("DB_PORT", "3306"),
		name,
	)
}

func migrateDB(db *sql.DB) error {
	raw, err := os.ReadFile("migrations/001_init.sql")
	if err != nil {
		return err
	}
	if _, err := db.Exec(string(raw)); err != nil {
		return err
	}
	_, _ = db.Exec("ALTER TABLE item_images MODIFY image_url MEDIUMTEXT NOT NULL")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN role ENUM('user', 'admin') NOT NULL DEFAULT 'user'")
	_, _ = db.Exec("ALTER TABLE items ADD COLUMN category_id BIGINT NOT NULL DEFAULT 801")
	_, _ = db.Exec("ALTER TABLE items ADD COLUMN seller_hidden BOOLEAN NOT NULL DEFAULT FALSE")
	_, _ = db.Exec("ALTER TABLE transactions ADD COLUMN buyer_hidden BOOLEAN NOT NULL DEFAULT FALSE")
	_, _ = db.Exec("ALTER TABLE transactions ADD COLUMN seller_hidden BOOLEAN NOT NULL DEFAULT FALSE")
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS item_likes (
			user_id BIGINT NOT NULL,
			item_id BIGINT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, item_id),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
		)
	`)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS item_views (
			id BIGINT PRIMARY KEY AUTO_INCREMENT,
			item_id BIGINT NOT NULL,
			viewer_id BIGINT NULL,
			viewer_hash CHAR(64) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_item_views_item_created (item_id, created_at),
			INDEX idx_item_views_viewer (item_id, viewer_hash, created_at),
			FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE,
			FOREIGN KEY (viewer_id) REFERENCES users(id) ON DELETE SET NULL
		)
	`)
	return nil
}

func loadStore(db *sql.DB, s *store) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	users, err := db.Query("SELECT id, email, password_hash, display_name, COALESCE(avatar_url, ''), COALESCE(role, 'user'), rating, created_at FROM users ORDER BY id")
	if err != nil {
		return err
	}
	defer users.Close()
	for users.Next() {
		var user User
		if err := users.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.DisplayName, &user.AvatarURL, &user.Role, &user.Rating, &user.CreatedAt); err != nil {
			return err
		}
		normalizeUserRole(&user)
		s.users[user.ID] = user
		s.usersByEmail[user.Email] = user.ID
		if user.ID >= s.nextUserID {
			s.nextUserID = user.ID + 1
		}
	}
	if err := users.Err(); err != nil {
		return err
	}

	items, err := db.Query("SELECT id, seller_id, title, description, price, shipping_fee, COALESCE(category_id, 801), category, status, condition_score, COALESCE(context, ''), COALESCE(seller_hidden, FALSE), created_at FROM items ORDER BY id")
	if err != nil {
		return err
	}
	defer items.Close()
	for items.Next() {
		var item Item
		if err := items.Scan(&item.ID, &item.SellerID, &item.Title, &item.Description, &item.Price, &item.ShippingFee, &item.CategoryID, &item.Category, &item.Status, &item.ConditionScore, &item.Context, &item.SellerHidden, &item.CreatedAt); err != nil {
			return err
		}
		normalizeItemCategory(&item)
		s.items[item.ID] = item
		if item.ID >= s.nextItemID {
			s.nextItemID = item.ID + 1
		}
	}
	if err := items.Err(); err != nil {
		return err
	}

	images, err := db.Query("SELECT item_id, image_url FROM item_images ORDER BY item_id, display_order, id")
	if err != nil {
		return err
	}
	defer images.Close()
	for images.Next() {
		var itemID int64
		var imageURL string
		if err := images.Scan(&itemID, &imageURL); err != nil {
			return err
		}
		item := s.items[itemID]
		item.Images = append(item.Images, imageURL)
		s.items[itemID] = item
	}
	if err := images.Err(); err != nil {
		return err
	}

	likes, err := db.Query("SELECT user_id, item_id FROM item_likes")
	if err != nil {
		return err
	}
	defer likes.Close()
	for likes.Next() {
		var userID, itemID int64
		if err := likes.Scan(&userID, &itemID); err != nil {
			return err
		}
		s.addLikeInMemory(userID, itemID)
	}
	if err := likes.Err(); err != nil {
		return err
	}

	views, err := db.Query("SELECT item_id, COALESCE(viewer_id, 0), viewer_hash, created_at FROM item_views ORDER BY item_id, created_at")
	if err != nil {
		return err
	}
	defer views.Close()
	for views.Next() {
		var view ItemView
		if err := views.Scan(&view.ItemID, &view.ViewerID, &view.ViewerHash, &view.CreatedAt); err != nil {
			return err
		}
		s.itemViews[view.ItemID] = append(s.itemViews[view.ItemID], view)
	}
	if err := views.Err(); err != nil {
		return err
	}

	txns, err := db.Query("SELECT id, item_id, buyer_id, seller_id, status, COALESCE(buyer_hidden, FALSE), COALESCE(seller_hidden, FALSE), created_at, completed_at FROM transactions ORDER BY id")
	if err != nil {
		return err
	}
	defer txns.Close()
	for txns.Next() {
		var txn Transaction
		var completedAt sql.NullTime
		if err := txns.Scan(&txn.ID, &txn.ItemID, &txn.BuyerID, &txn.SellerID, &txn.Status, &txn.BuyerHidden, &txn.SellerHidden, &txn.CreatedAt, &completedAt); err != nil {
			return err
		}
		if completedAt.Valid {
			txn.CompletedAt = &completedAt.Time
		}
		s.transactions[txn.ID] = txn
		if txn.ID >= s.nextTxnID {
			s.nextTxnID = txn.ID + 1
		}
	}
	if err := txns.Err(); err != nil {
		return err
	}

	msgs, err := db.Query("SELECT id, transaction_id, sender_id, body, sent_at FROM messages ORDER BY transaction_id, sent_at, id")
	if err != nil {
		return err
	}
	defer msgs.Close()
	for msgs.Next() {
		var msg Message
		if err := msgs.Scan(&msg.ID, &msg.TransactionID, &msg.SenderID, &msg.Body, &msg.SentAt); err != nil {
			return err
		}
		s.messages[msg.TransactionID] = append(s.messages[msg.TransactionID], msg)
		if msg.ID >= s.nextMsgID {
			s.nextMsgID = msg.ID + 1
		}
	}
	if err := msgs.Err(); err != nil {
		return err
	}

	reviews, err := db.Query("SELECT id, transaction_id, reviewer_id, reviewee_id, rating, COALESCE(comment, '') FROM reviews ORDER BY id")
	if err != nil {
		return err
	}
	defer reviews.Close()
	for reviews.Next() {
		var review Review
		if err := reviews.Scan(&review.ID, &review.TransactionID, &review.ReviewerID, &review.RevieweeID, &review.Rating, &review.Comment); err != nil {
			return err
		}
		s.reviews[review.ID] = review
		if review.ID >= s.nextReviewID {
			s.nextReviewID = review.ID + 1
		}
	}
	if err := reviews.Err(); err != nil {
		return err
	}
	for userID := range s.users {
		if err := s.recalculateUserRating(userID); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) persistAll() error {
	if s.db == nil {
		return nil
	}
	for _, user := range s.users {
		if err := s.saveUser(user); err != nil {
			return err
		}
	}
	for _, item := range s.items {
		if err := s.saveItem(item); err != nil {
			return err
		}
	}
	for _, txn := range s.transactions {
		if err := s.saveTransaction(txn); err != nil {
			return err
		}
	}
	for _, msgs := range s.messages {
		for _, msg := range msgs {
			if err := s.saveMessage(msg); err != nil {
				return err
			}
		}
	}
	for _, review := range s.reviews {
		if err := s.saveReview(review); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) saveUser(user User) error {
	if s.db == nil {
		return nil
	}
	normalizeUserRole(&user)
	_, err := s.db.Exec(`
		INSERT INTO users (id, email, password_hash, display_name, avatar_url, role, rating, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE email = VALUES(email), password_hash = VALUES(password_hash), display_name = VALUES(display_name), avatar_url = VALUES(avatar_url), role = VALUES(role), rating = VALUES(rating)
	`, user.ID, user.Email, user.PasswordHash, user.DisplayName, user.AvatarURL, user.Role, user.Rating, user.CreatedAt)
	return err
}

func (s *store) saveItem(item Item) error {
	if s.db == nil {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
		INSERT INTO items (id, seller_id, title, description, price, shipping_fee, category_id, category, status, condition_score, context, seller_hidden, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE title = VALUES(title), description = VALUES(description), price = VALUES(price), shipping_fee = VALUES(shipping_fee), category_id = VALUES(category_id), category = VALUES(category), status = VALUES(status), condition_score = VALUES(condition_score), context = VALUES(context), seller_hidden = VALUES(seller_hidden)
	`, item.ID, item.SellerID, item.Title, item.Description, item.Price, item.ShippingFee, item.CategoryID, item.Category, item.Status, item.ConditionScore, item.Context, item.SellerHidden, item.CreatedAt)
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM item_images WHERE item_id = ?", item.ID); err != nil {
		return err
	}
	for i, imageURL := range item.Images {
		if _, err := tx.Exec("INSERT INTO item_images (item_id, image_url, display_order) VALUES (?, ?, ?)", item.ID, imageURL, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *store) saveTransaction(txn Transaction) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO transactions (id, item_id, buyer_id, seller_id, status, buyer_hidden, seller_hidden, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE status = VALUES(status), buyer_hidden = VALUES(buyer_hidden), seller_hidden = VALUES(seller_hidden), completed_at = VALUES(completed_at)
	`, txn.ID, txn.ItemID, txn.BuyerID, txn.SellerID, txn.Status, txn.BuyerHidden, txn.SellerHidden, txn.CreatedAt, txn.CompletedAt)
	return err
}

func (s *store) saveMessage(msg Message) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO messages (id, transaction_id, sender_id, body, sent_at)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE body = VALUES(body), sent_at = VALUES(sent_at)
	`, msg.ID, msg.TransactionID, msg.SenderID, msg.Body, msg.SentAt)
	return err
}

func (s *store) saveReview(review Review) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO reviews (id, transaction_id, reviewer_id, reviewee_id, rating, comment)
		VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE rating = VALUES(rating), comment = VALUES(comment)
	`, review.ID, review.TransactionID, review.ReviewerID, review.RevieweeID, review.Rating, review.Comment)
	return err
}

func (s *store) saveLike(userID, itemID int64) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec("INSERT IGNORE INTO item_likes (user_id, item_id) VALUES (?, ?)", userID, itemID)
	return err
}

func (s *store) deleteLike(userID, itemID int64) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec("DELETE FROM item_likes WHERE user_id = ? AND item_id = ?", userID, itemID)
	return err
}

func (s *store) saveItemView(view ItemView) error {
	if s.db == nil {
		return nil
	}
	var viewerID any
	if view.ViewerID > 0 {
		viewerID = view.ViewerID
	}
	_, err := s.db.Exec(
		"INSERT INTO item_views (item_id, viewer_id, viewer_hash, created_at) VALUES (?, ?, ?, ?)",
		view.ItemID,
		viewerID,
		view.ViewerHash,
		view.CreatedAt,
	)
	return err
}

func (s *store) purgeItemByTitle(title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var itemIDs []int64
	for id, item := range s.items {
		if item.Title == title {
			itemIDs = append(itemIDs, id)
		}
	}
	if len(itemIDs) == 0 {
		return nil
	}
	itemSet := map[int64]bool{}
	for _, id := range itemIDs {
		itemSet[id] = true
	}
	var txnIDs []int64
	for id, txn := range s.transactions {
		if itemSet[txn.ItemID] {
			txnIDs = append(txnIDs, id)
		}
	}
	txnSet := map[int64]bool{}
	for _, id := range txnIDs {
		txnSet[id] = true
	}
	if s.db != nil {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, txnID := range txnIDs {
			if _, err := tx.Exec("DELETE FROM reviews WHERE transaction_id = ?", txnID); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM messages WHERE transaction_id = ?", txnID); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM transactions WHERE id = ?", txnID); err != nil {
				return err
			}
		}
		for _, itemID := range itemIDs {
			if _, err := tx.Exec("DELETE FROM item_images WHERE item_id = ?", itemID); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM items WHERE id = ?", itemID); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	for _, txnID := range txnIDs {
		delete(s.transactions, txnID)
		delete(s.messages, txnID)
	}
	for reviewID, review := range s.reviews {
		if txnSet[review.TransactionID] {
			delete(s.reviews, reviewID)
		}
	}
	for _, itemID := range itemIDs {
		delete(s.items, itemID)
		delete(s.itemLikes, itemID)
		delete(s.itemViews, itemID)
	}
	for userID := range s.users {
		if err := s.recalculateUserRating(userID); err != nil {
			return err
		}
	}
	return nil
}
