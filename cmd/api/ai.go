package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
)

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
	if !geminiConfigured() {
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
	text, err := callGeminiWithImages(ctx, os.Getenv("GEMINI_API_KEY"), geminiModel(), prompt, true, images)
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

func checkListingWithAI(ctx context.Context, item Item) (ListingCheckResult, error) {
	if !geminiConfigured() {
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
	text, err := callGeminiWithImages(ctx, os.Getenv("GEMINI_API_KEY"), geminiModel(), prompt, true, images)
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

type mockAssistant struct{}

func newAssistant() ListingAssistant {
	if geminiConfigured() {
		return geminiAssistant{
			apiKey: os.Getenv("GEMINI_API_KEY"),
			model:  geminiModel(),
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
	if !geminiConfigured() {
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
	return callGeminiWithImages(ctx, os.Getenv("GEMINI_API_KEY"), geminiModel(), prompt, false, firstGeminiImage(ctx, item.Images))
}

type geminiInlineImage struct {
	MimeType string
	Data     string
}

func callGemini(ctx context.Context, apiKey, model, prompt string, jsonOnly bool) (string, error) {
	return callGeminiWithImages(ctx, apiKey, model, prompt, jsonOnly, nil)
}

func geminiProvider() string {
	provider := strings.ToLower(strings.TrimSpace(env("GEMINI_PROVIDER", "")))
	if provider == "" {
		if os.Getenv("GEMINI_API_KEY") != "" {
			return "api-key"
		}
		return "mock"
	}
	return provider
}

func geminiModel() string {
	return env("GEMINI_MODEL", "gemini-2.5-pro")
}

func vertexProjectID() string {
	return env("GCP_PROJECT_ID", "astute-harbor-499700-p3")
}

func vertexLocation() string {
	return env("VERTEX_AI_LOCATION", "global")
}

func geminiConfigured() bool {
	switch geminiProvider() {
	case "vertex":
		return strings.TrimSpace(vertexProjectID()) != ""
	case "api-key":
		return os.Getenv("GEMINI_API_KEY") != ""
	default:
		return false
	}
}

func vertexGenerateContentURL(model string) string {
	location := vertexLocation()
	endpoint := "https://aiplatform.googleapis.com/v1"
	if location != "global" {
		endpoint = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1", location)
	}
	return fmt.Sprintf("%s/projects/%s/locations/%s/publishers/google/models/%s:generateContent", endpoint, vertexProjectID(), location, model)
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
	provider := geminiProvider()
	parts := []map[string]any{{"text": prompt}}
	for _, image := range images {
		if image.MimeType == "" || image.Data == "" {
			continue
		}
		if provider == "vertex" {
			parts = append(parts, map[string]any{
				"inlineData": map[string]string{
					"mimeType": image.MimeType,
					"data":     image.Data,
				},
			})
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
			{"role": "user", "parts": parts},
		},
	}
	if jsonOnly {
		body["generationConfig"] = map[string]any{"responseMimeType": "application/json"}
	}
	raw, _ := json.Marshal(body)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
	var token string
	if provider == "vertex" {
		source, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return "", err
		}
		accessToken, err := source.Token()
		if err != nil {
			return "", err
		}
		token = accessToken.AccessToken
		url = vertexGenerateContentURL(model)
	} else if apiKey == "" {
		return "", errors.New("GEMINI_API_KEY is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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
		if geminiProvider() == "vertex" {
			return errors.New("Vertex AIの権限またはモデル利用設定を確認してください。")
		}
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
