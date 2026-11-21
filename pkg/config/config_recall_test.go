package config

import (
	"testing"
)

func TestEmbeddingModelAndToolOutputBudgets(t *testing.T) {
	t.Setenv(EnvSelfID, "")
	path := writeTempConfig(t, `llm:
  models:
    - name: chat
      model: chat-v1
    - name: embeddings
      provider: openai
      base_url: https://example.invalid/v1
      model: embed-v2
  chat_model: chat
  embedding_model: embeddings
tools:
  enabled: [recall_memory]
  max_output_chars: 1800
  max_total_output_chars: 5000
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.LLM.EmbeddingEntry()
	if !ok || entry.Name != "embeddings" || entry.Model != "embed-v2" {
		t.Fatalf("embedding_model 引用解析错误: entry=%+v ok=%v", entry, ok)
	}
	if cfg.Tools.MaxOutputChars != 1800 || cfg.Tools.MaxTotalOutputChars != 5000 {
		t.Fatalf("工具输出预算解析错误: %+v", cfg.Tools)
	}

	cfg.LLM.EmbeddingModel = ""
	if _, ok := cfg.LLM.EmbeddingEntry(); ok {
		t.Fatal("embedding_model 为空时不得回退到 models[0]")
	}
	cfg.LLM.EmbeddingModel = "missing"
	if _, ok := cfg.LLM.EmbeddingEntry(); ok {
		t.Fatal("embedding_model 引用未命中时应返回 false")
	}
}

func TestDefaultToolOutputBudgets(t *testing.T) {
	cfg, err := Load(t.TempDir() + "/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.MaxOutputChars != 2500 || cfg.Tools.MaxTotalOutputChars != 6000 {
		t.Fatalf("默认工具输出预算 = %+v，期望单次 2500、合计 6000", cfg.Tools)
	}
	if cfg.LLM.EmbeddingModel != "" {
		t.Fatalf("默认配置应关闭向量模型，实际 embedding_model=%q", cfg.LLM.EmbeddingModel)
	}
}
