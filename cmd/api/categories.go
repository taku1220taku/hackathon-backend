package main

import (
	"fmt"
	"strings"
)

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
