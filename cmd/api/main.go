package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

type app struct {
	store     *store
	jwtSecret []byte
	ai        ListingAssistant
}

type store struct {
	db           *sql.DB
	mu           sync.RWMutex
	nextUserID   int64
	nextItemID   int64
	nextTxnID    int64
	nextMsgID    int64
	nextReviewID int64
	users        map[int64]User
	usersByEmail map[string]int64
	items        map[int64]Item
	transactions map[int64]Transaction
	messages     map[int64][]Message
	reviews      map[int64]Review
}

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"displayName"`
	AvatarURL    string    `json:"avatarUrl"`
	Rating       float64   `json:"rating"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Item struct {
	ID              int64     `json:"id"`
	SellerID        int64     `json:"sellerId"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Price           int       `json:"price"`
	ShippingFee     int       `json:"shippingFee"`
	CategoryID      int64     `json:"categoryId"`
	Category        string    `json:"category"`
	Status          string    `json:"status"`
	ConditionScore  int       `json:"conditionScore"`
	Context         string    `json:"context"`
	Images          []string  `json:"images"`
	SellerCanDelete bool      `json:"sellerCanDelete"`
	SellerHidden    bool      `json:"-"`
	CreatedAt       time.Time `json:"createdAt"`
}

type Transaction struct {
	ID              int64      `json:"id"`
	ItemID          int64      `json:"itemId"`
	BuyerID         int64      `json:"buyerId"`
	SellerID        int64      `json:"sellerId"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	Item            *Item      `json:"item,omitempty"`
	MyReviewed      bool       `json:"myReviewed"`
	PartnerReviewed bool       `json:"partnerReviewed"`
	ItemUnavailable bool       `json:"itemUnavailable"`
	UnavailableText string     `json:"unavailableText,omitempty"`
	BuyerHidden     bool       `json:"-"`
	SellerHidden    bool       `json:"-"`
}

type Message struct {
	ID            int64     `json:"id"`
	TransactionID int64     `json:"transactionId"`
	SenderID      int64     `json:"senderId"`
	Body          string    `json:"body"`
	SentAt        time.Time `json:"sentAt"`
}

type Review struct {
	ID            int64  `json:"id"`
	TransactionID int64  `json:"transactionId"`
	ReviewerID    int64  `json:"reviewerId"`
	RevieweeID    int64  `json:"revieweeId"`
	Rating        int    `json:"rating"`
	Comment       string `json:"comment"`
	ReviewerName  string `json:"reviewerName,omitempty"`
	ReviewerRole  string `json:"reviewerRole,omitempty"`
	RevieweeName  string `json:"revieweeName,omitempty"`
	RevieweeRole  string `json:"revieweeRole,omitempty"`
}

type ListingAssistRequest struct {
	ImageURL string `json:"imageUrl"`
	Memo     string `json:"memo"`
}

type ItemQuestionRequest struct {
	ItemID   int64  `json:"itemId"`
	Question string `json:"question"`
}

type ItemQuestionResult struct {
	Answer string `json:"answer"`
}

type ListingAssistResult struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	CategoryID      int64    `json:"categoryId"`
	Category        string   `json:"category"`
	ConditionScore  int      `json:"conditionScore"`
	ConditionNotes  string   `json:"conditionNotes"`
	SuggestedTags   []string `json:"suggestedTags"`
	SuggestedPrice  int      `json:"suggestedPrice"`
	SellThroughDays int      `json:"sellThroughDays"`
}

type ListingAssistant interface {
	Assist(r ListingAssistRequest) (ListingAssistResult, error)
}

type CategoryDef struct {
	ID    int64
	Label string
}

var categoryDefs = []CategoryDef{
	{101, "レディース / トップス"},
	{102, "レディース / ジャケット/アウター"},
	{103, "レディース / バッグ"},
	{104, "レディース / 靴"},
	{201, "メンズ / トップス"},
	{202, "メンズ / ジャケット/アウター"},
	{203, "メンズ / バッグ"},
	{204, "メンズ / 靴"},
	{301, "家電・スマホ・カメラ / スマートフォン/携帯電話"},
	{302, "家電・スマホ・カメラ / PC/タブレット"},
	{303, "家電・スマホ・カメラ / カメラ"},
	{304, "家電・スマホ・カメラ / オーディオ機器"},
	{401, "本・音楽・ゲーム / 本"},
	{402, "本・音楽・ゲーム / 漫画"},
	{403, "本・音楽・ゲーム / CD/DVD/ブルーレイ"},
	{404, "本・音楽・ゲーム / ゲーム"},
	{501, "おもちゃ・ホビー・グッズ / キャラクターグッズ"},
	{502, "おもちゃ・ホビー・グッズ / 楽器/機材"},
	{503, "おもちゃ・ホビー・グッズ / トレーディングカード"},
	{601, "スポーツ・レジャー / アウトドア"},
	{602, "スポーツ・レジャー / スポーツ用品"},
	{701, "コスメ・香水・美容 / ベースメイク"},
	{702, "コスメ・香水・美容 / 香水"},
	{801, "その他 / その他"},
}

func categoryLabelByID(id int64) string {
	for _, category := range categoryDefs {
		if category.ID == id {
			return category.Label
		}
	}
	return "その他 / その他"
}

func categoryIDByLabel(label string) int64 {
	normalized := strings.ToLower(strings.TrimSpace(label))
	for _, category := range categoryDefs {
		if strings.ToLower(category.Label) == normalized {
			return category.ID
		}
	}
	return 801
}

func categoryPromptList() string {
	parts := make([]string, 0, len(categoryDefs))
	for _, category := range categoryDefs {
		parts = append(parts, fmt.Sprintf("%d:%s", category.ID, category.Label))
	}
	return strings.Join(parts, ", ")
}

func normalizeItemCategory(item *Item) {
	if item.CategoryID == 0 && strings.TrimSpace(item.Category) != "" {
		item.CategoryID = categoryIDByLabel(item.Category)
	}
	if item.CategoryID == 0 {
		item.CategoryID = 801
	}
	item.Category = categoryLabelByID(item.CategoryID)
}

func normalizeAssistCategory(result *ListingAssistResult) {
	if result.CategoryID == 0 && strings.TrimSpace(result.Category) != "" {
		result.CategoryID = categoryIDByLabel(result.Category)
	}
	if result.CategoryID == 0 {
		result.CategoryID = 801
	}
	result.Category = categoryLabelByID(result.CategoryID)
	if result.ConditionScore < 0 {
		result.ConditionScore = 0
	}
	if result.ConditionScore > 100 {
		result.ConditionScore = 100
	}
}

func main() {
	s := newStore()
	if db, err := openDatabase(); err != nil {
		log.Printf("MySQL disabled: %v", err)
	} else {
		s.db = db
		if err := migrateDB(db); err != nil {
			log.Fatalf("failed to migrate database: %v", err)
		}
		if err := loadStore(db, s); err != nil {
			log.Fatalf("failed to load database: %v", err)
		}
		if err := s.purgeItemByTitle("Persistence Test Jacket"); err != nil {
			log.Fatalf("failed to remove obsolete item: %v", err)
		}
		log.Printf("MySQL persistence enabled")
	}
	if len(s.users) == 0 {
		seed(s)
		if err := s.persistAll(); err != nil {
			log.Fatalf("failed to seed database: %v", err)
		}
	}
	a := &app{
		store:     s,
		jwtSecret: []byte(env("JWT_SECRET", "dev-secret-change-me")),
		ai:        newAssistant(),
	}

	mux := http.NewServeMux()
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("POST /auth/register", a.register)
	mux.HandleFunc("POST /auth/login", a.login)
	mux.HandleFunc("GET /me", a.requireAuth(a.me))
	mux.HandleFunc("PATCH /me", a.requireAuth(a.updateMe))
	mux.HandleFunc("GET /me/items", a.requireAuth(a.listMyItems))
	mux.HandleFunc("GET /me/reviews", a.requireAuth(a.listMyReviews))
	mux.HandleFunc("POST /uploads", a.requireAuth(a.uploadImage))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(env("UPLOAD_DIR", "uploads")))))
	mux.HandleFunc("GET /items", a.listItems)
	mux.HandleFunc("POST /items", a.requireAuth(a.createItem))
	mux.HandleFunc("GET /items/{id}", a.getItem)
	mux.HandleFunc("PATCH /items/{id}", a.requireAuth(a.updateItem))
	mux.HandleFunc("DELETE /items/{id}", a.requireAuth(a.deleteItem))
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
	mux.HandleFunc("POST /ai/fraud-check", a.requireAuth(a.fraudCheck))
	mux.HandleFunc("POST /ai/item-question", a.requireAuth(a.itemQuestion))
	mux.HandleFunc("GET /ai/gemini-status", a.requireAuth(a.geminiStatus))
	mux.HandleFunc("GET /ai/recommendations", a.requireAuth(a.recommendations))

	addr := ":" + env("PORT", "8080")
	log.Printf("CapCycle API listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, withCORS(mux)))
}

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
		transactions: map[int64]Transaction{},
		messages:     map[int64][]Message{},
		reviews:      map[int64]Review{},
	}
}

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
	_, _ = db.Exec("ALTER TABLE items ADD COLUMN category_id BIGINT NOT NULL DEFAULT 801")
	_, _ = db.Exec("ALTER TABLE items ADD COLUMN seller_hidden BOOLEAN NOT NULL DEFAULT FALSE")
	_, _ = db.Exec("ALTER TABLE transactions ADD COLUMN buyer_hidden BOOLEAN NOT NULL DEFAULT FALSE")
	_, _ = db.Exec("ALTER TABLE transactions ADD COLUMN seller_hidden BOOLEAN NOT NULL DEFAULT FALSE")
	return nil
}

func loadStore(db *sql.DB, s *store) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	users, err := db.Query("SELECT id, email, password_hash, display_name, COALESCE(avatar_url, ''), rating, created_at FROM users ORDER BY id")
	if err != nil {
		return err
	}
	defer users.Close()
	for users.Next() {
		var user User
		if err := users.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.DisplayName, &user.AvatarURL, &user.Rating, &user.CreatedAt); err != nil {
			return err
		}
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
	_, err := s.db.Exec(`
		INSERT INTO users (id, email, password_hash, display_name, avatar_url, rating, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE email = VALUES(email), password_hash = VALUES(password_hash), display_name = VALUES(display_name), avatar_url = VALUES(avatar_url), rating = VALUES(rating)
	`, user.ID, user.Email, user.PasswordHash, user.DisplayName, user.AvatarURL, user.Rating, user.CreatedAt)
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
	return item
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
	}
	for userID := range s.users {
		if err := s.recalculateUserRating(userID); err != nil {
			return err
		}
	}
	return nil
}

func seed(s *store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	user := User{ID: s.nextUserID, Email: "demo@capcycle.test", PasswordHash: string(hash), DisplayName: "Kumazo", AvatarURL: "", Rating: 4.8, CreatedAt: time.Now()}
	s.nextUserID++
	s.users[user.ID] = user
	s.usersByEmail[user.Email] = user.ID
	buyer := User{ID: s.nextUserID, Email: "buyer@capcycle.test", PasswordHash: string(hash), DisplayName: "Buyer", AvatarURL: "", Rating: 5, CreatedAt: time.Now()}
	s.nextUserID++
	s.users[buyer.ID] = buyer
	s.usersByEmail[buyer.Email] = buyer.ID
	for _, item := range []Item{
		{Title: "Vintage Coach Bag", CategoryID: 103, Category: "レディース / バッグ", Price: 16800, ShippingFee: 700, ConditionScore: 82, Context: "角スレ小、通学サイズ、90s leather", Images: []string{"https://images.unsplash.com/photo-1594223274512-ad4803739b7c?auto=format&fit=crop&w=900&q=80"}},
		{Title: "Canon Autoboy", CategoryID: 303, Category: "家電・スマホ・カメラ / カメラ", Price: 12400, ShippingFee: 520, ConditionScore: 74, Context: "動作確認済み、レンズ内チリ少", Images: []string{"https://images.unsplash.com/photo-1512790182412-b19e6d62bc39?auto=format&fit=crop&w=900&q=80"}},
		{Title: "Band Hoodie 2024", CategoryID: 201, Category: "メンズ / トップス", Price: 5900, ShippingFee: 850, ConditionScore: 68, Context: "袖に薄い汚れ、限定会場販売", Images: []string{"https://images.unsplash.com/photo-1556821840-3a63f95609a7?auto=format&fit=crop&w=900&q=80"}},
	} {
		item.ID = s.nextItemID
		s.nextItemID++
		item.SellerID = user.ID
		item.Description = item.Context
		item.Status = "published"
		item.CreatedAt = time.Now()
		s.items[item.ID] = item
	}
}

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
	user := User{ID: a.store.nextUserID, Email: req.Email, PasswordHash: string(hash), DisplayName: req.DisplayName, Rating: 5, CreatedAt: time.Now()}
	a.store.nextUserID++
	a.store.users[user.ID] = user
	a.store.usersByEmail[user.Email] = user.ID
	if err := a.store.saveUser(user); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save user")
		return
	}
	token := a.signToken(user.ID)
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
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "token": a.signToken(user.ID)})
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
		all = append(all, item)
	}
	a.store.mu.RUnlock()
	sort.SliceStable(all, func(i, j int) bool {
		switch sortMode {
		case "popular":
			return all[i].ConditionScore > all[j].ConditionScore
		case "recommended":
			left := all[i].ConditionScore*100 - all[i].Price/100
			right := all[j].ConditionScore*100 - all[j].Price/100
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
	a.store.mu.RLock()
	item, ok := a.store.items[id]
	a.store.mu.RUnlock()
	if !ok || item.SellerHidden || item.Status == "draft" {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
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
	writeJSON(w, http.StatusCreated, req)
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
	writeJSON(w, http.StatusOK, item)
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
			item := a.store.items[txn.ItemID]
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

func (a *app) listingAssist(w http.ResponseWriter, r *http.Request, user User) {
	var req ListingAssistRequest
	if !decode(w, r, &req) {
		return
	}
	result, err := a.ai.Assist(req)
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

type PriceSuggestionResult struct {
	SuggestedPrice  int   `json:"suggestedPrice"`
	MarketRange     []int `json:"marketRange"`
	SellThroughDays int   `json:"sellThroughDays"`
}

type ListingCheckResult struct {
	Risk    string   `json:"risk"`
	Reasons []string `json:"reasons"`
}

func suggestPriceWithAI(ctx context.Context, title, description string, categoryID int64, category string, conditionScore, targetSellDays int, imageURLs []string) (PriceSuggestionResult, error) {
	if categoryID == 0 {
		categoryID = categoryIDByLabel(category)
	}
	category = categoryLabelByID(categoryID)
	if conditionScore <= 0 {
		conditionScore = 75
	}
	if targetSellDays <= 0 {
		targetSellDays = 7
	}
	if targetSellDays > 60 {
		targetSellDays = 60
	}
	if os.Getenv("GEMINI_API_KEY") == "" {
		return heuristicPriceSuggestion(category, conditionScore, targetSellDays), nil
	}
	prompt := fmt.Sprintf(
		"あなたはCapCycleの中古相場アナリストです。JSONのみを返してください。schema: {\"suggestedPrice\":number,\"marketRange\":number[],\"sellThroughDays\":number}。marketRangeは[min,max]の2要素。日本円で、10〜20代向けフリマの売れやすさを重視してください。ユーザーは約%d日以内に売りたいので、その日数に合う価格を提案してください。\n商品名: %s\n説明: %s\nカテゴリID: %d\nカテゴリ: %s\n状態スコア: %d",
		targetSellDays,
		title,
		description,
		categoryID,
		category,
		conditionScore,
	)
	images := firstGeminiImage(ctx, imageURLs)
	text, err := callGeminiWithImages(ctx, os.Getenv("GEMINI_API_KEY"), env("GEMINI_MODEL", "gemini-2.5-flash"), prompt, true, images)
	if err != nil {
		return PriceSuggestionResult{}, err
	}
	var result PriceSuggestionResult
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		return PriceSuggestionResult{}, err
	}
	if result.SuggestedPrice <= 0 || len(result.MarketRange) != 2 || result.SellThroughDays <= 0 {
		return PriceSuggestionResult{}, errors.New("invalid gemini price suggestion")
	}
	return result, nil
}

func heuristicPriceSuggestion(category string, conditionScore, targetSellDays int) PriceSuggestionResult {
	base := 6800
	switch {
	case strings.Contains(category, "スマートフォン"), strings.Contains(category, "PC"), strings.Contains(category, "カメラ"), strings.Contains(category, "オーディオ"):
		base = 18000
	case strings.Contains(category, "バッグ"):
		base = 12000
	case strings.Contains(category, "靴"):
		base = 8000
	case strings.Contains(category, "ゲーム"):
		base = 6500
	case strings.Contains(category, "トレーディングカード"):
		base = 4200
	case strings.Contains(category, "コスメ"), strings.Contains(category, "香水"):
		base = 3800
	case strings.Contains(category, "本"), strings.Contains(category, "漫画"):
		base = 1800
	}
	price := base * max(conditionScore, 50) / 80
	switch {
	case targetSellDays <= 3:
		price = price * 85 / 100
	case targetSellDays <= 7:
		price = price * 95 / 100
	case targetSellDays >= 21:
		price = price * 110 / 100
	}
	return PriceSuggestionResult{
		SuggestedPrice:  price,
		MarketRange:     []int{price * 85 / 100, price * 115 / 100},
		SellThroughDays: targetSellDays,
	}
}

func checkListingWithAI(ctx context.Context, item Item) (ListingCheckResult, error) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		risk := "low"
		reasons := []string{"禁止ワードなし", "価格帯は許容範囲"}
		if item.Price > 500000 || strings.Contains(item.Description, "偽物") {
			risk = "watch"
			reasons = append(reasons, "人手確認が必要です")
		}
		return ListingCheckResult{Risk: risk, Reasons: reasons}, nil
	}
	prompt := fmt.Sprintf(
		"あなたはCapCycleの出品品質チェックAIです。JSONのみを返してください。schema: {\"risk\":\"low|watch|high\",\"reasons\":string[]}。禁止品、禁止ワード、異常価格、重複を疑わせる表現、説明不足、状態説明の不足を確認してください。断定しすぎず、購入者保護の観点で短い理由を返してください。\n商品名: %s\n説明: %s\nカテゴリ: %s\n価格: %d\n状態スコア: %d\n画像枚数: %d",
		item.Title,
		item.Description,
		item.Category,
		item.Price,
		item.ConditionScore,
		len(item.Images),
	)
	images := firstGeminiImage(ctx, item.Images)
	text, err := callGeminiWithImages(ctx, os.Getenv("GEMINI_API_KEY"), env("GEMINI_MODEL", "gemini-2.5-flash"), prompt, true, images)
	if err != nil {
		return ListingCheckResult{}, err
	}
	var result ListingCheckResult
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		return ListingCheckResult{}, err
	}
	if result.Risk != "low" && result.Risk != "watch" && result.Risk != "high" {
		result.Risk = "watch"
	}
	if len(result.Reasons) == 0 {
		result.Reasons = []string{"AIチェック結果の理由が空でした"}
	}
	return result, nil
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
	apiKey := os.Getenv("GEMINI_API_KEY")
	model := env("GEMINI_MODEL", "gemini-2.5-flash")
	result := map[string]any{
		"configured": apiKey != "",
		"model":      model,
		"live":       false,
	}
	if apiKey == "" || r.URL.Query().Get("live") != "1" {
		writeJSON(w, http.StatusOK, result)
		return
	}
	answer, err := callGemini(r.Context(), apiKey, model, "CapCycle Gemini connection check. Reply with OK only.", false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"configured": true,
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
			all = append(all, item)
		}
	}
	a.store.mu.RUnlock()
	sort.SliceStable(all, func(i, j int) bool {
		left := categoryWeight[all[i].Category]*10000 + all[i].ConditionScore*100 - all[i].Price/100
		right := categoryWeight[all[j].Category]*10000 + all[j].ConditionScore*100 - all[j].Price/100
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

func (a *app) canAccessTransaction(txnID, userID int64) bool {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	txn, ok := a.store.transactions[txnID]
	return ok && (txn.BuyerID == userID || txn.SellerID == userID)
}

func (a *app) requireAuth(next func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		userID, err := a.verifyToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		a.store.mu.RLock()
		user, ok := a.store.users[userID]
		a.store.mu.RUnlock()
		if !ok {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}
		next(w, r, user)
	}
}

func (a *app) signToken(userID int64) string {
	header := b64(`{"alg":"HS256","typ":"JWT"}`)
	payload := b64(fmt.Sprintf(`{"sub":%d,"exp":%d}`, userID, time.Now().Add(24*time.Hour).Unix()))
	body := header + "." + payload
	mac := hmac.New(sha256.New, a.jwtSecret)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *app) verifyToken(token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, errors.New("invalid token")
	}
	body := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, a.jwtSecret)
	mac.Write([]byte(body))
	if !hmac.Equal([]byte(parts[2]), []byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))) {
		return 0, errors.New("invalid signature")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, err
	}
	var payload struct {
		Sub int64 `json:"sub"`
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return 0, err
	}
	if time.Now().Unix() > payload.Exp {
		return 0, errors.New("expired token")
	}
	return payload.Sub, nil
}

type mockAssistant struct{}

func newAssistant() ListingAssistant {
	if os.Getenv("GEMINI_API_KEY") != "" {
		return geminiAssistant{
			apiKey: os.Getenv("GEMINI_API_KEY"),
			model:  env("GEMINI_MODEL", "gemini-2.5-flash"),
		}
	}
	return mockAssistant{}
}

func (mockAssistant) Assist(r ListingAssistRequest) (ListingAssistResult, error) {
	memo := strings.TrimSpace(r.Memo)
	if memo == "" {
		memo = "写真から角スレと使用感を検出"
	}
	return ListingAssistResult{
		Title:           "AI診断 レザーショルダー",
		Description:     memo + "。日常使いしやすいサイズ感で、状態スコアに基づいた透明な説明文を生成しました。",
		CategoryID:      103,
		Category:        "レディース / バッグ",
		ConditionScore:  82,
		ConditionNotes:  "小さなスレあり。目立つ破損は検出されませんでした。",
		SuggestedTags:   []string{"通学", "90s", "レザー", "状態診断済み"},
		SuggestedPrice:  16800,
		SellThroughDays: 4,
	}, nil
}

type geminiAssistant struct {
	apiKey string
	model  string
}

func (a geminiAssistant) Assist(r ListingAssistRequest) (ListingAssistResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	prompt := "あなたはCapCycleのフリマ出品補助AIです。JSONのみを返してください。" +
		"schema: {\"title\":string,\"description\":string,\"categoryId\":number,\"category\":string,\"conditionScore\":number,\"conditionNotes\":string,\"suggestedTags\":string[],\"suggestedPrice\":number,\"sellThroughDays\":number}。" +
		"categoryIdとcategoryは次の一覧から最も近いものを1つ選んでください: " + categoryPromptList() + "。conditionScoreは0-100。" +
		"\nユーザーメモ: " + r.Memo
	images := []geminiInlineImage{}
	if image, err := fetchGeminiImage(ctx, r.ImageURL); err == nil {
		images = append(images, image)
	} else if strings.TrimSpace(r.ImageURL) != "" {
		prompt += "\n画像URL: " + r.ImageURL + "\n注記: 画像本体の取得に失敗したため、URLとメモから推定してください。"
	}
	text, err := callGeminiWithImages(ctx, a.apiKey, a.model, prompt, true, images)
	if err != nil {
		return ListingAssistResult{}, err
	}
	var result ListingAssistResult
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		return ListingAssistResult{}, err
	}
	normalizeAssistCategory(&result)
	return result, nil
}

func answerItemQuestion(ctx context.Context, item Item, question string) (string, error) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		return "商品説明と状態スコアを見る限り、気になる点は取引メッセージで出品者に確認するのがおすすめです。", nil
	}
	prompt := fmt.Sprintf(
		"あなたはフリマ購入を支援するAIです。商品情報だけを根拠に、短く正直に回答してください。不明な点は不明と伝えてください。\n商品名: %s\nカテゴリ: %s\n価格: %d\n状態スコア: %d\n説明: %s\n文脈: %s\n質問: %s",
		item.Title,
		item.Category,
		item.Price,
		item.ConditionScore,
		item.Description,
		item.Context,
		question,
	)
	return callGeminiWithImages(ctx, os.Getenv("GEMINI_API_KEY"), env("GEMINI_MODEL", "gemini-2.5-flash"), prompt, false, firstGeminiImage(ctx, item.Images))
}

type geminiInlineImage struct {
	MimeType string
	Data     string
}

func callGemini(ctx context.Context, apiKey, model, prompt string, jsonOnly bool) (string, error) {
	return callGeminiWithImages(ctx, apiKey, model, prompt, jsonOnly, nil)
}

func firstGeminiImage(ctx context.Context, imageURLs []string) []geminiInlineImage {
	if len(imageURLs) == 0 {
		return nil
	}
	image, err := fetchGeminiImage(ctx, imageURLs[0])
	if err != nil {
		return nil
	}
	return []geminiInlineImage{image}
}

func callGeminiWithImages(ctx context.Context, apiKey, model, prompt string, jsonOnly bool, images []geminiInlineImage) (string, error) {
	parts := []map[string]any{{"text": prompt}}
	for _, image := range images {
		if image.MimeType == "" || image.Data == "" {
			continue
		}
		parts = append(parts, map[string]any{
			"inline_data": map[string]string{
				"mime_type": image.MimeType,
				"data":      image.Data,
			},
		})
	}
	body := map[string]any{
		"contents": []map[string]any{
			{"parts": parts},
		},
	}
	if jsonOnly {
		body["generationConfig"] = map[string]any{"responseMimeType": "application/json"}
	}
	raw, _ := json.Marshal(body)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", friendlyGeminiError(resp.StatusCode, respBytes)
	}
	var wrapped struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBytes, &wrapped); err != nil {
		return "", errors.New("invalid gemini response")
	}
	for _, candidate := range wrapped.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				return part.Text, nil
			}
		}
	}
	return "", errors.New("gemini response did not include text")
}

func fetchGeminiImage(ctx context.Context, imageURL string) (geminiInlineImage, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return geminiInlineImage{}, errors.New("empty image url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return geminiInlineImage{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return geminiInlineImage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return geminiInlineImage{}, fmt.Errorf("image fetch failed: %d", resp.StatusCode)
	}
	mimeType := resp.Header.Get("Content-Type")
	if index := strings.Index(mimeType, ";"); index >= 0 {
		mimeType = mimeType[:index]
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return geminiInlineImage{}, errors.New("image url did not return an image")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return geminiInlineImage{}, err
	}
	if len(raw) == 0 {
		return geminiInlineImage{}, errors.New("empty image body")
	}
	return geminiInlineImage{MimeType: mimeType, Data: base64.StdEncoding.EncodeToString(raw)}, nil
}

func friendlyGeminiError(statusCode int, body []byte) error {
	var provider struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &provider)
	switch {
	case statusCode == http.StatusServiceUnavailable || provider.Error.Status == "UNAVAILABLE":
		return errors.New("Geminiが混雑しています。少し待ってからもう一度試してください。")
	case statusCode == http.StatusTooManyRequests || provider.Error.Status == "RESOURCE_EXHAUSTED":
		return errors.New("Gemini APIの利用上限に達しました。時間を置いて再試行してください。")
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return errors.New("Gemini APIキーを確認してください。")
	case provider.Error.Message != "":
		return fmt.Errorf("Gemini APIエラー: %s", provider.Error.Message)
	default:
		return fmt.Errorf("Gemini APIエラーが発生しました (%d)", statusCode)
	}
}

func cleanJSON(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "error": nil})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": nil, "error": message})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, allowed := range strings.Split(env("CORS_ORIGIN", "http://localhost:5173,http://localhost:5174"), ",") {
			if strings.TrimSpace(allowed) == origin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				break
			}
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func b64(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ *sql.DB
