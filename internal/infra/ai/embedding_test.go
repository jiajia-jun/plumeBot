package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"plumebot/pkg/config"
)

func TestOpenAIEmbedderBatchesAndKeysVectorDimensions(t *testing.T) {
	var mu sync.Mutex
	dimension := 2
	var gotInput []string
	var gotModel, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("解析 embedding 请求: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		gotInput = append([]string(nil), request.Input...)
		gotModel = request.Model
		gotAuthorization = r.Header.Get("Authorization")
		currentDimension := dimension
		mu.Unlock()
		data := make([]map[string]any, len(request.Input))
		for i := range request.Input {
			vector := make([]float32, currentDimension)
			vector[0] = 1
			data[i] = map[string]any{"index": i, "embedding": vector}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "model": request.Model, "data": data})
	}))
	defer server.Close()

	adapter, err := NewOpenAIEmbedder(config.LLMModelConfig{
		Name: "embed", Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "test-secret", Model: "embed-v2",
	}, 5)
	if err != nil {
		t.Fatal(err)
	}
	initialKey := adapter.ModelKey()
	vectors, err := adapter.Embed(context.Background(), []string{"第一段", "第二段"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || !reflect.DeepEqual(vectors[0], []float32{1, 0}) || !reflect.DeepEqual(vectors[1], []float32{1, 0}) {
		t.Fatalf("embedding 响应顺序/值错误: %+v", vectors)
	}
	mu.Lock()
	input, model, authorization := append([]string(nil), gotInput...), gotModel, gotAuthorization
	mu.Unlock()
	if !reflect.DeepEqual(input, []string{"第一段", "第二段"}) || model != "embed-v2" || authorization != "Bearer test-secret" {
		t.Fatalf("embedding 请求内容错误: input=%v model=%q authorization=%q", input, model, authorization)
	}
	dimensionKey := adapter.ModelKey()
	if dimensionKey != initialKey || adapter.Dimensions() != 2 {
		t.Fatalf("ModelKey 应稳定并记录维数：key=%q initial=%q dimensions=%d", dimensionKey, initialKey, adapter.Dimensions())
	}
	if again := adapter.ModelKey(); again != dimensionKey {
		t.Fatalf("相同模型与维数的 ModelKey 不稳定: %q / %q", dimensionKey, again)
	}

	mu.Lock()
	dimension = 3
	mu.Unlock()
	if _, err := adapter.Embed(context.Background(), []string{"变化维度"}); err == nil {
		t.Fatal("运行期间模型维数变化必须显式失败，避免混用向量空间")
	}
}

func TestNewOpenAIEmbedderRejectsUnsupportedProvider(t *testing.T) {
	if _, err := NewOpenAIEmbedder(config.LLMModelConfig{Provider: "other", Model: "embedding"}, 1); err == nil {
		t.Fatal("不支持的 embedding provider 应返回错误")
	}
}
