package main

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

func seed(s *store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	user := User{ID: s.nextUserID, Email: "demo@capcycle.test", PasswordHash: string(hash), DisplayName: "Kumazo", AvatarURL: "", Role: "user", Rating: 4.8, CreatedAt: time.Now()}
	s.nextUserID++
	s.users[user.ID] = user
	s.usersByEmail[user.Email] = user.ID
	buyer := User{ID: s.nextUserID, Email: "buyer@capcycle.test", PasswordHash: string(hash), DisplayName: "Buyer", AvatarURL: "", Role: "user", Rating: 5, CreatedAt: time.Now()}
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
