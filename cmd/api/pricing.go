package main

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

type collectibleProfile struct {
	Multiplier  float64
	Signals     []string
	RiskSignals []string
}

var (
	eraPattern   = regexp.MustCompile(`(19[5-9][0-9]|20[0-1][0-9])年代|(?:19[5-9][0-9]|20[0-1][0-9])s`)
	modelPattern = regexp.MustCompile(`[A-Z]{2,}[- ]?\d{2,}[A-Z0-9-]*`)
)

func heuristicPriceSuggestion(title, description, category string, conditionScore, targetSellDays int) PriceSuggestionResult {
	profile := collectibleProfileFor(title, description, category, conditionScore)
	base := 6800
	text := strings.ToLower(title + " " + description + " " + category)
	switch {
	case strings.Contains(text, "gibson") && strings.Contains(text, "les paul") && strings.Contains(text, "custom shop"):
		base = 620000
	case strings.Contains(text, "gibson") && strings.Contains(text, "les paul"):
		base = 280000
	case strings.Contains(text, "ts-808"), strings.Contains(text, "tube screamer"):
		base = 85000
	case strings.Contains(text, "rolex"):
		base = 450000
	case strings.Contains(text, "leica"):
		base = 180000
	case strings.Contains(category, "スマートフォン"), strings.Contains(category, "PC"), strings.Contains(category, "カメラ"), strings.Contains(category, "オーディオ"):
		base = 18000
	case strings.Contains(category, "楽器"), strings.Contains(text, "ギター"), strings.Contains(text, "guitar"), strings.Contains(text, "エフェクター"):
		base = 42000
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
	price := int(float64(base*max(conditionScore, 50)/80) * profile.Multiplier)
	switch {
	case targetSellDays <= 3:
		if len(profile.Signals) > 0 {
			price = price * 93 / 100
		} else {
			price = price * 85 / 100
		}
	case targetSellDays <= 7:
		if len(profile.Signals) > 0 {
			price = price * 98 / 100
		} else {
			price = price * 95 / 100
		}
	case targetSellDays >= 21:
		price = price * 110 / 100
	}
	price = roundPrice(price)
	rangeLow := price * 85 / 100
	rangeHigh := price * 115 / 100
	if len(profile.Signals) > 0 {
		rangeLow = price * 88 / 100
		rangeHigh = price * 128 / 100
	}
	return PriceSuggestionResult{
		SuggestedPrice:  price,
		MarketRange:     []int{roundPrice(rangeLow), roundPrice(rangeHigh)},
		SellThroughDays: targetSellDays,
	}
}

func solveDynamicPrice(req DynamicPriceRequest) DynamicPriceResult {
	profile := collectibleProfileFor(req.Title, req.Description, req.Category, req.ConditionScore)
	market := heuristicPriceSuggestion(req.Title, req.Description, req.Category, req.ConditionScore, req.TargetSellDays)
	if len(req.MarketRange) == 2 && req.MarketRange[0] > 0 && req.MarketRange[1] >= req.MarketRange[0] {
		market.MarketRange = []int{req.MarketRange[0], req.MarketRange[1]}
	}
	if len(profile.Signals) > 0 && req.CurrentPrice > market.MarketRange[1] {
		market.MarketRange[0] = max(market.MarketRange[0], req.CurrentPrice*75/100)
		market.MarketRange[1] = max(market.MarketRange[1], req.CurrentPrice*125/100)
		market.SuggestedPrice = max(market.SuggestedPrice, req.CurrentPrice*95/100)
	}
	marketMid := max(1, (market.MarketRange[0]+market.MarketRange[1]+req.CurrentPrice)/3)
	minimumPrice := req.MinimumPrice
	if minimumPrice <= 0 {
		marketFloor := market.MarketRange[0] * 90 / 100
		if marketFloor >= req.CurrentPrice {
			marketFloor = req.CurrentPrice * 75 / 100
		}
		minimumPrice = max(marketFloor, req.CurrentPrice*75/100)
		if len(profile.Signals) > 0 {
			collectibleFloor := market.MarketRange[0] * 95 / 100
			if collectibleFloor >= req.CurrentPrice {
				collectibleFloor = req.CurrentPrice * 85 / 100
			}
			minimumPrice = max(collectibleFloor, req.CurrentPrice*85/100)
		}
	}
	minimumPrice = min(req.CurrentPrice, max(100, minimumPrice))
	candidates := priceCandidates(minimumPrice, req.CurrentPrice)
	if !containsInt(candidates, req.CurrentPrice) {
		candidates = append(candidates, req.CurrentPrice)
		sort.Ints(candidates)
	}
	days := req.TargetSellDays
	values := make([][]float64, days+1)
	choices := make([][]int, days)
	for day := range values {
		values[day] = make([]float64, len(candidates))
	}
	for day := range choices {
		choices[day] = make([]int, len(candidates))
	}
	for day := days - 1; day >= 1; day-- {
		remainingUrgency := 1 + float64(day)/float64(max(days, 1))*0.2
		for maxIndex := range candidates {
			bestValue := -1.0
			bestIndex := 0
			for priceIndex := 0; priceIndex <= maxIndex; priceIndex++ {
				price := candidates[priceIndex]
				lambda := saleIntensity(price, marketMid, req, remainingUrgency)
				value := lambda*float64(price) + (1-lambda)*values[day+1][priceIndex]
				if value > bestValue {
					bestValue = value
					bestIndex = priceIndex
				}
			}
			values[day][maxIndex] = bestValue
			choices[day][maxIndex] = bestIndex
		}
	}
	chosenPrices := make([]int, days)
	chosenLambda := make([]float64, days)
	currentIndex := sort.SearchInts(candidates, req.CurrentPrice)
	chosenPrices[0] = req.CurrentPrice
	chosenLambda[0] = saleIntensity(req.CurrentPrice, marketMid, req, 1)
	for day := 1; day < days; day++ {
		currentIndex = choices[day][currentIndex]
		chosenPrices[day] = candidates[currentIndex]
		urgency := 1 + float64(day)/float64(max(days, 1))*0.2
		chosenLambda[day] = saleIntensity(chosenPrices[day], marketMid, req, urgency)
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
	if len(profile.Signals) > 0 {
		confidence += 0.05
	}
	confidence = math.Round(math.Min(0.92, confidence)*100) / 100
	explanation := fmt.Sprintf("閲覧%d件、直近%d件、いいね%d件を需要強度に反映し、%d日以内の期待収益が最大になる価格軌道を計算しました。", req.ViewCount, req.RecentViewCount, req.LikeCount, days)
	if len(profile.Signals) > 0 {
		explanation = fmt.Sprintf("%s 希少性シグナル（%s）を加味し、下げすぎない価格帯に補正しています。", explanation, strings.Join(profile.Signals, "、"))
	}
	return DynamicPriceResult{
		RecommendedPrice: chosenPrices[0],
		ExpectedSellDays: expectedSellDays,
		PricePath:        path,
		MarketRange:      []int{market.MarketRange[0], market.MarketRange[1]},
		Confidence:       confidence,
		Explanation:      explanation,
	}
}

func applyCollectiblePriceFloor(result, heuristic PriceSuggestionResult, profile collectibleProfile) PriceSuggestionResult {
	if len(profile.Signals) == 0 || len(result.MarketRange) != 2 || len(heuristic.MarketRange) != 2 {
		return result
	}
	floor := heuristic.SuggestedPrice * 85 / 100
	if result.SuggestedPrice < floor {
		result.SuggestedPrice = roundPrice(floor)
	}
	lowFloor := heuristic.MarketRange[0] * 85 / 100
	highFloor := heuristic.MarketRange[1] * 85 / 100
	if result.MarketRange[0] < lowFloor {
		result.MarketRange[0] = roundPrice(lowFloor)
	}
	if result.MarketRange[1] < highFloor {
		result.MarketRange[1] = roundPrice(highFloor)
	}
	if result.MarketRange[1] < result.SuggestedPrice {
		result.MarketRange[1] = roundPrice(result.SuggestedPrice * 115 / 100)
	}
	if result.MarketRange[0] > result.SuggestedPrice {
		result.MarketRange[0] = roundPrice(result.SuggestedPrice * 85 / 100)
	}
	return result
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
	if profile := collectibleProfileFor(req.Title, req.Description, req.Category, req.ConditionScore); len(profile.Signals) > 0 {
		engagement *= 1 + math.Min(0.28, (profile.Multiplier-1)*0.35)
	}
	targetFactor := math.Max(0.7, math.Min(1.5, 8.0/float64(max(req.TargetSellDays, 1))))
	elasticity := 2.2
	lambda := 0.08 * conditionFactor * engagement * targetFactor * urgency * math.Exp(-elasticity*(priceRatio-1))
	return math.Max(0.01, math.Min(0.85, lambda))
}

func collectibleProfileFor(title, description, category string, conditionScore int) collectibleProfile {
	raw := title + " " + description + " " + category
	text := strings.ToLower(raw)
	score := 0.0
	signals := []string{}
	risks := []string{}
	addSignal := func(label string, weight float64) {
		if !containsString(signals, label) {
			signals = append(signals, label)
			score += weight
		}
	}
	addRisk := func(label string, penalty float64) {
		if !containsString(risks, label) {
			risks = append(risks, label)
			score -= penalty
		}
	}

	if strings.Contains(text, "ヴィンテージ") || strings.Contains(text, "ビンテージ") || strings.Contains(text, "vintage") {
		addSignal("ヴィンテージ", 0.22)
	}
	if eraPattern.MatchString(text) {
		addSignal("年代物", 0.18)
	}
	if strings.Contains(text, "廃番") || strings.Contains(text, "生産終了") || strings.Contains(text, "discontinued") {
		addSignal("廃番", 0.18)
	}
	if strings.Contains(text, "限定") || strings.Contains(text, "limited") {
		addSignal("限定", 0.14)
	}
	if strings.Contains(text, "初期") || strings.Contains(text, "early") || strings.Contains(text, "first") {
		addSignal("初期仕様", 0.12)
	}
	if strings.Contains(text, "custom shop") || strings.Contains(text, "カスタムショップ") {
		addSignal("Custom Shop", 0.2)
	}
	if strings.Contains(text, "made in japan") || strings.Contains(text, "日本製") || strings.Contains(text, "made in usa") || strings.Contains(text, "usa製") {
		addSignal("製造国", 0.1)
	}
	if strings.Contains(text, "希少") || strings.Contains(text, "レア") || strings.Contains(text, "当時物") || strings.Contains(text, "オリジナル") {
		addSignal("希少性", 0.12)
	}
	if modelPattern.MatchString(raw) {
		addSignal("型番あり", 0.08)
	}
	for _, brand := range []string{"gibson", "fender", "ibanez", "boss", "leica", "rolex", "omega", "cartier", "louis vuitton", "chanel", "hermes"} {
		if strings.Contains(text, brand) {
			addSignal("コレクター需要ブランド", 0.14)
			break
		}
	}
	for _, risk := range []string{"復刻", "レプリカ", "ジャンク", "動作未確認", "欠品", "改造", "リペア", "社外", "偽物", "コピー"} {
		if strings.Contains(text, risk) {
			addRisk(risk, 0.08)
		}
	}

	capMultiplier := 1.55
	if strings.Contains(text, "gibson") || strings.Contains(text, "fender") || strings.Contains(text, "ibanez") || strings.Contains(text, "rolex") || strings.Contains(text, "leica") ||
		strings.Contains(category, "楽器") || strings.Contains(category, "カメラ") || strings.Contains(category, "時計") || strings.Contains(category, "バッグ") || strings.Contains(category, "トレーディングカード") {
		capMultiplier = 1.85
	}
	if conditionScore > 0 && conditionScore < 50 {
		capMultiplier = math.Min(capMultiplier, 1.35)
	}
	multiplier := math.Max(1, math.Min(capMultiplier, 1+score))
	return collectibleProfile{Multiplier: multiplier, Signals: signals, RiskSignals: risks}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func roundPrice(price int) int {
	if price < 1000 {
		return max(100, int(math.Round(float64(price)/50))*50)
	}
	return max(100, int(math.Round(float64(price)/100))*100)
}
