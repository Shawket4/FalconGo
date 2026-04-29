package AbstractFunctions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const TEIEndpoint = "http://localhost:8080/embed" 

type TEIBatchRequest struct {
	Inputs []string `json:"inputs"`
}

// GenerateEmbeddingsBatch takes an array of strings and returns an array of vectors
func GenerateEmbeddingsBatch(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := TEIBatchRequest{Inputs: texts}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal TEI request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second} 
	resp, err := client.Post(TEIEndpoint, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("TEI request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TEI returned status: %d", resp.StatusCode)
	}

	var embeddings [][]float32
	if err := json.NewDecoder(resp.Body).Decode(&embeddings); err != nil {
		return nil, fmt.Errorf("failed to decode TEI response: %w", err)
	}

	return embeddings, nil
}

// GenerateEmbedding is a convenience wrapper for single string queries
func GenerateEmbedding(text string) ([]float32, error) {
	embeddings, err := GenerateEmbeddingsBatch([]string{text})
	if err != nil || len(embeddings) == 0 {
		return nil, err
	}
	return embeddings[0], nil
}