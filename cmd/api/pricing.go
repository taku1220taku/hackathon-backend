package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

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

func solveDynamicPrice(req DynamicPriceRequest) DynamicPriceResult {
	market := heuristicPriceSuggestion(req.Category, req.ConditionScore, req.TargetSellDays)
	marketMid := max(1, (market.MarketRange[0]+market.MarketRange[1]+req.CurrentPrice)/3)
	minimumPrice := req.MinimumPrice
	if minimumPrice <= 0 {
		minimumPrice = max(market.MarketRange[0]*90/100, req.CurrentPrice*75/100)
	}
	minimumPrice = max(100, minimumPrice)
	maximumPrice := max(req.CurrentPrice*130/100, market.MarketRange[1]*115/100)
	if maximumPrice < minimumPrice {
		maximumPrice = minimumPrice + 1000
	}
	candidates := priceCandidates(minimumPrice, maximumPrice)
	days := req.TargetSellDays
	values := make([]float64, days+1)
	chosenPrices := make([]int, days)
	chosenLambda := make([]float64, days)
	for day := days - 1; day >= 0; day-- {
		bestValue := -1.0
		bestPrice := candidates[0]
		bestLambda := 0.0
		remainingUrgency := 1 + float64(day)/float64(max(days, 1))*0.2
		for _, price := range candidates {
			lambda := saleIntensity(price, marketMid, req, remainingUrgency)
			value := lambda*float64(price) + (1-lambda)*values[day+1]
			if value > bestValue {
				bestValue = value
				bestPrice = price
				bestLambda = lambda
			}
		}
		values[day] = bestValue
		chosenPrices[day] = bestPrice
		chosenLambda[day] = bestLambda
	}
	path := make([]DynamicPricePoint, 0, days)
	notSold := 1.0
	expectedSellDays := days
	for day := 0; day < days; day++ {
		cumulative := 1 - notSold*(1-chosenLambda[day])
		path = append(path, DynamicPricePoint{
			Day:             day + 1,
			Price:           chosenPrices[day],
			SellProbability: math.Round(cumulative*1000) / 10,
		})
		if expectedSellDays == days && cumulative >= 0.5 {
			expectedSellDays = day + 1
		}
		notSold *= 1 - chosenLambda[day]
	}
	confidence := 0.45 + math.Min(0.3, math.Log(float64(req.ViewCount+1))*0.06) + math.Min(0.18, float64(req.LikeCount)*0.025) + math.Min(0.07, req.ViewVelocity*0.015)
	confidence = math.Round(math.Min(0.92, confidence)*100) / 100
	explanation := fmt.Sprintf("閲覧%d件、直近%d件、いいね%d件を需要強度に反映し、%d日以内の期待収益が最大になる価格軌道を計算しました。", req.ViewCount, req.RecentViewCount, req.LikeCount, days)
	return DynamicPriceResult{
		RecommendedPrice: chosenPrices[0],
		ExpectedSellDays: expectedSellDays,
		PricePath:        path,
		MarketRange:      []int{market.MarketRange[0], market.MarketRange[1]},
		Confidence:       confidence,
		Explanation:      explanation,
	}
}

func priceCandidates(minimumPrice, maximumPrice int) []int {
	steps := 15
	if maximumPrice <= minimumPrice {
		return []int{roundPrice(minimumPrice)}
	}
	candidates := make([]int, 0, steps+1)
	seen := map[int]bool{}
	for i := 0; i <= steps; i++ {
		price := minimumPrice + (maximumPrice-minimumPrice)*i/steps
		price = roundPrice(price)
		if price < minimumPrice {
			price = minimumPrice
		}
		if !seen[price] {
			seen[price] = true
			candidates = append(candidates, price)
		}
	}
	sort.Ints(candidates)
	return candidates
}

func saleIntensity(price, marketMid int, req DynamicPriceRequest, urgency float64) float64 {
	priceRatio := float64(price) / float64(max(marketMid, 1))
	conditionFactor := 0.55 + math.Min(1, float64(max(req.ConditionScore, 40))/100)*0.75
	engagement := 1.0 + math.Min(0.9, float64(req.LikeCount)*0.055) + math.Min(0.7, float64(req.RecentViewCount)*0.035) + math.Min(0.8, req.ViewVelocity*0.08) + math.Min(0.35, math.Log(float64(req.ViewCount+1))*0.045)
	targetFactor := math.Max(0.7, math.Min(1.5, 8.0/float64(max(req.TargetSellDays, 1))))
	elasticity := 2.2
	lambda := 0.08 * conditionFactor * engagement * targetFactor * urgency * math.Exp(-elasticity*(priceRatio-1))
	return math.Max(0.01, math.Min(0.85, lambda))
}

func roundPrice(price int) int {
	if price < 1000 {
		return max(100, int(math.Round(float64(price)/50))*50)
	}
	return max(100, int(math.Round(float64(price)/100))*100)
}
