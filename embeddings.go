package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var embeddingCache = map[string][]float32{}

func jinaEmbedSingle(text, apiKey string) ([]float32, error) {
	if v, ok := embeddingCache["jina:"+text]; ok {
		return v, nil
	}
	payload := map[string]interface{}{
		"model":      "jina-embeddings-v3",
		"task":       "retrieval.passage",
		"dimensions": 512,
		"input":      []string{text},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.jina.ai/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jina %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("jina empty")
	}
	v := out.Data[0].Embedding
	embeddingCache["jina:"+text] = v
	return v, nil
}

func jinaEmbedQuery(text, apiKey string) ([]float32, error) {
	if v, ok := embeddingCache["jina-q:"+text]; ok {
		return v, nil
	}
	payload := map[string]interface{}{
		"model":      "jina-embeddings-v3",
		"task":       "retrieval.query",
		"dimensions": 512,
		"input":      []string{text},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.jina.ai/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jina %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("jina empty")
	}
	v := out.Data[0].Embedding
	embeddingCache["jina-q:"+text] = v
	return v, nil
}

func cohereEmbedSingle(text, apiKey string) ([]float32, error) {
	if v, ok := embeddingCache["cohere:"+text]; ok {
		return v, nil
	}
	payload := map[string]interface{}{
		"model":      "embed-english-v3.0",
		"input_type": "search_document",
		"texts":      []string{text},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.cohere.com/v1/embed", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cohere %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("cohere empty")
	}
	v := out.Embeddings[0]
	embeddingCache["cohere:"+text] = v
	return v, nil
}

func cohereEmbedQuery(text, apiKey string) ([]float32, error) {
	if v, ok := embeddingCache["cohere-q:"+text]; ok {
		return v, nil
	}
	payload := map[string]interface{}{
		"model":      "embed-english-v3.0",
		"input_type": "search_query",
		"texts":      []string{text},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api.cohere.com/v1/embed", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cohere %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("cohere empty")
	}
	v := out.Embeddings[0]
	embeddingCache["cohere-q:"+text] = v
	return v, nil
}

func embeddingProvider() string {
	if os.Getenv("JINA_API_KEY") != "" {
		return "jina"
	}
	if os.Getenv("COHERE_API_KEY") != "" {
		return "cohere"
	}
	return "hash"
}

func vecForDoc(text string) []float32 {
	toks := tok(text)
	joined := strings.Join(toks, " ")
	if key := os.Getenv("JINA_API_KEY"); key != "" {
		if v, err := jinaEmbedSingle(joined, key); err == nil {
			return v
		}
	}
	if key := os.Getenv("COHERE_API_KEY"); key != "" {
		if v, err := cohereEmbedSingle(joined, key); err == nil {
			return v
		}
	}
	return vecOfHash(toks)
}

func vecForQuery(text string) []float32 {
	toks := tok(text)
	joined := strings.Join(toks, " ")
	if key := os.Getenv("JINA_API_KEY"); key != "" {
		if v, err := jinaEmbedQuery(joined, key); err == nil {
			return v
		}
	}
	if key := os.Getenv("COHERE_API_KEY"); key != "" {
		if v, err := cohereEmbedQuery(joined, key); err == nil {
			return v
		}
	}
	return vecOfHash(toks)
}
