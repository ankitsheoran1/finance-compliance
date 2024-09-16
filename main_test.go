package main

import (
	"bytes"
	"fmt"
	"github.com/sashabaranov/go-openai"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAnalyze(t *testing.T) {
	// Prepare query parameters based on the followup prompt
	query := "policy=https://docs.stripe.com/treasury/marketing-treasury&webpage=https://mercury.com/"
	reqBody := bytes.NewBufferString(query)

	// Create a request to pass to our handler with the correct query parameters.
	req, err := http.NewRequest("POST", "/compliance?"+query, reqBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Create a ResponseRecorder to record the response.
	rr := httptest.NewRecorder()
	// Create a new APIServer instance for testing.
	storage := NewMemoryStorage()
	config, _ := ReadConfig()
	openAiClient := openai.NewClient(os.Getenv("OPENAPI_KEY"))
	listenAddr := fmt.Sprintf(":%d", config.Port)
	server := NewAPIServer(listenAddr, storage, openAiClient, config)

	// Call the analyze function directly.
	server.analyze(rr, req)

	fmt.Println("", rr.Body.String())

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	// Check the response body length is as expected.
	if len(rr.Body.String()) <= 100 {
		t.Errorf("handler returned unexpected body length: got %v want greater than 100",
			len(rr.Body.String()))
	}
}
