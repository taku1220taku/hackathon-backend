package main

import (
	"context"
	"database/sql"
	"sync"
	"time"
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
	itemLikes    map[int64]map[int64]bool
	itemViews    map[int64][]ItemView
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
	Role         string    `json:"role"`
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
	LikeCount       int       `json:"likeCount"`
	LikedByMe       bool      `json:"likedByMe"`
	ViewCount       int       `json:"viewCount"`
	RecentViewCount int       `json:"recentViewCount"`
	ViewVelocity    float64   `json:"viewVelocity"`
	SellerHidden    bool      `json:"-"`
	CreatedAt       time.Time `json:"createdAt"`
}

type ItemView struct {
	ItemID     int64
	ViewerID   int64
	ViewerHash string
	CreatedAt  time.Time
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

type ItemMetrics struct {
	ViewCount       int     `json:"viewCount"`
	RecentViewCount int     `json:"recentViewCount"`
	ViewVelocity    float64 `json:"viewVelocity"`
	LikeCount       int     `json:"likeCount"`
}

type DynamicPriceRequest struct {
	ItemID          int64   `json:"itemId"`
	Title           string  `json:"title"`
	CategoryID      int64   `json:"categoryId"`
	Category        string  `json:"category"`
	CurrentPrice    int     `json:"currentPrice"`
	ConditionScore  int     `json:"conditionScore"`
	LikeCount       int     `json:"likeCount"`
	ViewCount       int     `json:"viewCount"`
	RecentViewCount int     `json:"recentViewCount"`
	ViewVelocity    float64 `json:"viewVelocity"`
	TargetSellDays  int     `json:"targetSellDays"`
	MinimumPrice    int     `json:"minimumPrice"`
}

type DynamicPricePoint struct {
	Day             int     `json:"day"`
	Price           int     `json:"price"`
	SellProbability float64 `json:"sellProbability"`
}

type DynamicPriceResult struct {
	RecommendedPrice int                 `json:"recommendedPrice"`
	ExpectedSellDays int                 `json:"expectedSellDays"`
	PricePath        []DynamicPricePoint `json:"pricePath"`
	MarketRange      []int               `json:"marketRange"`
	Confidence       float64             `json:"confidence"`
	Explanation      string              `json:"explanation"`
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
	Assist(ctx context.Context, r ListingAssistRequest) (ListingAssistResult, error)
}
