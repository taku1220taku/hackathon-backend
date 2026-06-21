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
