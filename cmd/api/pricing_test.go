package main

import (
	"strings"
	"testing"
)

func TestHeuristicPriceSuggestionAppliesCollectiblePremium(t *testing.T) {
	regular := heuristicPriceSuggestion("Ibanez エフェクター", "通常の中古エフェクターです。", "その他 / その他", 80, 7)
	vintage := heuristicPriceSuggestion("Ibanez TS-808 Tube Screamer", "1980年代ごろの初期仕様。廃番で希少なヴィンテージ個体です。", "その他 / その他", 80, 7)

	if vintage.SuggestedPrice <= regular.SuggestedPrice {
		t.Fatalf("expected vintage price > regular price, got vintage=%d regular=%d", vintage.SuggestedPrice, regular.SuggestedPrice)
	}
	if vintage.MarketRange[0] <= regular.MarketRange[0] {
		t.Fatalf("expected vintage market floor > regular market floor, got vintage=%d regular=%d", vintage.MarketRange[0], regular.MarketRange[0])
	}
}

func TestDynamicPricePreservesCollectibleCurrentPrice(t *testing.T) {
	result := solveDynamicPrice(DynamicPriceRequest{
		Title:          "Gibson Les Paul Custom Shop",
		Description:    "1990年代にCustom Shopで製造された希少なヴィンテージ個体です。",
		Category:       "その他 / その他",
		CurrentPrice:   800000,
		ConditionScore: 80,
		TargetSellDays: 7,
	})

	if result.MarketRange[0] < 600000 {
		t.Fatalf("expected collectible market floor to stay near current price, got %d", result.MarketRange[0])
	}
	if result.RecommendedPrice < 680000 {
		t.Fatalf("expected collectible recommended price not to over-discount, got %d", result.RecommendedPrice)
	}
	if !strings.Contains(result.Explanation, "希少性シグナル") {
		t.Fatalf("expected explanation to mention collectible adjustment, got %q", result.Explanation)
	}
}

func TestDynamicPriceStartsAtSuggestedPriceAndNeverIncreases(t *testing.T) {
	result := solveDynamicPrice(DynamicPriceRequest{
		Title:           "ProCo RAT 2 ディストーション",
		Description:     "目立つ傷や汚れはなく、全体的に綺麗な状態です。",
		CategoryID:      502,
		Category:        "おもちゃ・ホビー・グッズ / 楽器/機材",
		CurrentPrice:    6800,
		MarketRange:     []int{6000, 8000},
		ConditionScore:  90,
		TargetSellDays:  7,
		ViewCount:       0,
		RecentViewCount: 0,
		LikeCount:       0,
	})

	if result.RecommendedPrice != 6800 {
		t.Fatalf("expected first price to match the suggested price, got %d", result.RecommendedPrice)
	}
	if len(result.PricePath) != 7 {
		t.Fatalf("expected 7 price points, got %d", len(result.PricePath))
	}
	for index, point := range result.PricePath {
		if index == 0 && point.Price != 6800 {
			t.Fatalf("expected day 1 price 6800, got %d", point.Price)
		}
		if index > 0 && point.Price > result.PricePath[index-1].Price {
			t.Fatalf("expected non-increasing prices, day %d rose from %d to %d", point.Day, result.PricePath[index-1].Price, point.Price)
		}
	}
	if result.MarketRange[0] != 6000 || result.MarketRange[1] != 8000 {
		t.Fatalf("expected provided market range, got %v", result.MarketRange)
	}
}
