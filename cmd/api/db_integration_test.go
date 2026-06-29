package main

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMySQLDraftSurvivesStoreReload(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("ping MySQL: %v", err)
	}
	if err := migrateDB(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}

	testID := time.Now().UnixNano()
	email := fmt.Sprintf("reload-%d@capcycle.test", testID)
	user := User{
		ID: testID, Email: email, PasswordHash: "test", DisplayName: "Reload Test",
		Role: "user", Rating: 5, CreatedAt: time.Now(),
	}
	item := Item{
		ID: testID, SellerID: user.ID, Title: "", Description: "", Price: 0,
		ShippingFee: 700, CategoryID: 801, Category: "その他 / その他",
		Status: "draft", ConditionScore: 0, Images: []string{},
		CreatedAt: time.Now(),
	}
	defer func() {
		_, _ = db.Exec("DELETE FROM item_images WHERE item_id = ?", item.ID)
		_, _ = db.Exec("DELETE FROM items WHERE id = ?", item.ID)
		_, _ = db.Exec("DELETE FROM users WHERE id = ?", user.ID)
	}()

	writer := newStore()
	writer.db = db
	if err := writer.saveUser(user); err != nil {
		t.Fatalf("save user: %v", err)
	}
	if err := writer.saveItem(item); err != nil {
		t.Fatalf("save draft: %v", err)
	}

	reloaded := newStore()
	reloaded.db = db
	if err := loadStore(db, reloaded); err != nil {
		t.Fatalf("reload store: %v", err)
	}
	got, ok := reloaded.items[item.ID]
	if !ok {
		t.Fatal("draft was not restored after store reload")
	}
	if got.Status != "draft" || got.Price != 0 || got.Title != "" {
		t.Fatalf("unexpected restored draft: %#v", got)
	}
}
