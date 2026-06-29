package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeTestItem(t *testing.T, recorder *httptest.ResponseRecorder) Item {
	t.Helper()
	var envelope struct {
		Data Item `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return envelope.Data
}

func TestIncompleteDraftCanBeCreatedAndLoadedByOwner(t *testing.T) {
	a := &app{store: newStore()}
	owner := User{ID: 1}
	request := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"status":"draft","images":[]}`))
	recorder := httptest.NewRecorder()

	a.createItem(recorder, request, owner)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	item := decodeTestItem(t, recorder)
	if item.Status != "draft" || item.Title != "" || item.Price != 0 {
		t.Fatalf("unexpected draft: %#v", item)
	}

	ownerRequest := httptest.NewRequest(http.MethodGet, "/me/items/1", nil)
	ownerRequest.SetPathValue("id", "1")
	ownerRecorder := httptest.NewRecorder()
	a.getMyItem(ownerRecorder, ownerRequest, owner)
	if ownerRecorder.Code != http.StatusOK {
		t.Fatalf("owner could not load draft: %d %s", ownerRecorder.Code, ownerRecorder.Body.String())
	}

	otherRecorder := httptest.NewRecorder()
	a.getMyItem(otherRecorder, ownerRequest, User{ID: 2})
	if otherRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected draft to be hidden from another user, got %d", otherRecorder.Code)
	}
}

func TestIncompleteDraftCannotBePublished(t *testing.T) {
	a := &app{store: newStore()}
	request := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"status":"published","images":[]}`))
	recorder := httptest.NewRecorder()

	a.createItem(recorder, request, User{ID: 1})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestItemUpdateCanClearImagesAndSetZeroValues(t *testing.T) {
	s := newStore()
	s.items[1] = Item{
		ID: 1, SellerID: 1, Title: "Draft", Description: "Body", Price: 1000,
		ShippingFee: 700, CategoryID: 801, Category: "その他 / その他",
		Status: "draft", ConditionScore: 80, Images: []string{"https://example.test/image.jpg"},
	}
	a := &app{store: s}
	request := httptest.NewRequest(http.MethodPatch, "/items/1", strings.NewReader(`{"title":"","price":0,"conditionScore":0,"images":[],"status":"draft"}`))
	request.SetPathValue("id", "1")
	recorder := httptest.NewRecorder()

	a.updateItem(recorder, request, User{ID: 1})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	item := decodeTestItem(t, recorder)
	if item.Title != "" || item.Price != 0 || item.ConditionScore != 0 || len(item.Images) != 0 {
		t.Fatalf("zero values were not applied: %#v", item)
	}
}
