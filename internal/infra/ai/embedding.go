package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	openai "github.com/meguminnnnnnnnn/go-openai"

	"plumebot/internal/domain"
	"plumebot/pkg/config"
)

var _ domain.Embedder = (*OpenAIEmbedder)(nil)

// OpenAIEmbedder 是 OpenAI 兼容 embeddings endpoint 的轻量适配器。
// 不记录输入文本、API Key 或响应正文。
type OpenAIEmbedder struct {
	client     *openai.Client
	model      string
	modelKey   string // provider/endpoint/model 指纹，不含 API key
	mu         sync.RWMutex
	dimensions int // 首次成功响应后纳入 ModelKey，防止同名服务维数变化时混用向量
}

func NewOpenAIEmbedder(entry config.LLMModelConfig, timeoutSeconds int) (*OpenAIEmbedder, error) {
	provider := entry.Provider
	if provider == "" {
		provider = config.DefaultLLMProvider
	}
	if provider != config.DefaultLLMProvider {
		return nil, fmt.Errorf("embedding provider %q 暂不支持；仅支持 openai 兼容接口", provider)
	}
	if strings.TrimSpace(entry.Model) == "" {
		return nil, errors.New("embedding 模型名未配置")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(entry.BaseURL), "/")
	if baseURL == "" {
		baseURL = config.DefaultOpenAIBaseURL
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = config.DefaultLLMTimeoutSeconds
	}
	clientConfig := openai.DefaultConfig(entry.APIKey)
	clientConfig.BaseURL = baseURL
	clientConfig.HTTPClient = &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	fingerprint := sha256.Sum256([]byte(provider + "\x00" + baseURL + "\x00" + entry.Model))
	return &OpenAIEmbedder{
		client: openai.NewClientWithConfig(clientConfig), model: entry.Model,
		modelKey: hex.EncodeToString(fingerprint[:]),
	}, nil
}

func (e *OpenAIEmbedder) ModelKey() string { return e.modelKey }

func (e *OpenAIEmbedder) Dimensions() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dimensions
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("embedding 输入不得为空")
		}
	}
	response, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequestStrings{
		Input: texts, Model: openai.EmbeddingModel(e.model),
	})
	if err != nil {
		return nil, fmt.Errorf("调用 embedding endpoint 失败: %w", err)
	}
	if len(response.Data) != len(texts) {
		return nil, fmt.Errorf("embedding 返回数量 %d 与输入数量 %d 不一致", len(response.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	seen := make([]bool, len(texts))
	dimensions := 0
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(texts) || seen[item.Index] {
			return nil, errors.New("embedding 响应包含重复或越界 index")
		}
		seen[item.Index] = true
		if len(item.Embedding) == 0 {
			return nil, errors.New("embedding 响应包含空向量")
		}
		if dimensions == 0 {
			dimensions = len(item.Embedding)
		} else if len(item.Embedding) != dimensions {
			return nil, errors.New("embedding 响应包含不同维数的向量")
		}
		vector := make([]float32, len(item.Embedding))
		var norm float64
		for n, value := range item.Embedding {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, errors.New("embedding 响应包含非有限向量值")
			}
			vector[n] = value
			norm += float64(value) * float64(value)
		}
		if norm == 0 {
			return nil, errors.New("embedding 响应包含零向量")
		}
		out[item.Index] = vector
	}
	for _, ok := range seen {
		if !ok {
			return nil, errors.New("embedding 响应缺少向量")
		}
	}
	e.mu.Lock()
	previousDimensions := e.dimensions
	if previousDimensions != 0 && previousDimensions != dimensions {
		e.mu.Unlock()
		return nil, fmt.Errorf("embedding 模型维数发生变化：%d -> %d", previousDimensions, dimensions)
	}
	e.dimensions = dimensions
	e.mu.Unlock()
	return out, nil
}
