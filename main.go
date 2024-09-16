package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"encoding/json"
	"github.com/spf13/viper"

	"github.com/PuerkitoBio/goquery"
	"github.com/gorilla/mux"
	"github.com/sashabaranov/go-openai"
)

type Storage interface {
	Insert(key string, value string) error
	Get(key string) (string, error)
}

type InMemory struct {
	db   map[string]string
	lock sync.RWMutex
}

type Config struct {
	OpenAI struct {
		Model  string `mapstructure:"model"`
		Tokens int    `mapstructure:"tokens"`
	} `mapstructure:"openai"`
	Port int `mapstructure:"port"`
	Prompt string `mapstructure:"prompt"`
}

func ReadConfig() (*Config, error) {
	viper.SetConfigName("config") // Name of the config file (without extension)
	viper.SetConfigType("yaml")   // File type is YAML
	viper.AddConfigPath(".")      // Look for the config file in the current directory

	// Read the config file
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	// Initialize the config struct
	var config Config

	// Unmarshal the config into the struct
	if err := viper.Unmarshal(&config); err != nil {
		log.Fatalf("Unable to decode into struct: %v", err)
	}

	return &config, nil

}

func NewMemoryStorage() *InMemory {
	return &InMemory{
		db:   make(map[string]string),
		lock: sync.RWMutex{},
	}
}

func (i *InMemory) Insert(key string, value string) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	if i.db == nil {
		i.db = make(map[string]string)
	}
	i.db[key] = value
	return nil
}

func (i *InMemory) Get(key string) (string, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()
	if val, ok := i.db[key]; ok {
		return val, nil
	}
	return "", fmt.Errorf("key not found")
}

type APIServer struct {
	listenAddr string
	store      Storage
	aiClient   *openai.Client
	config     *Config
}

func NewAPIServer(listenAddr string, store Storage, openaiClient *openai.Client, config *Config) *APIServer {
	return &APIServer{
		listenAddr: listenAddr,
		store:      store,
		aiClient:   openaiClient,
		config:     config,
	}
}

func (s *APIServer) Run() {
	router := mux.NewRouter()
	router.Use(Logger)
	router.HandleFunc("/compliance", s.analyze).Methods("POST")
	fmt.Println("API server running on port: ", s.listenAddr)
	http.ListenAndServe(s.listenAddr, router)
}

func createCacheKey(policy string, webpage string) string {
	return policy + webpage
}

func (s *APIServer) analyze(w http.ResponseWriter, r *http.Request) {
	policy := r.URL.Query().Get("policy")
	webpage := r.URL.Query().Get("webpage")

	if policy == "" || webpage == "" {
		http.Error(w, "Invalid input: policy is invalid", http.StatusBadRequest)
		return
	}

	// Create a key from the policy and webpage URLs
	key := createCacheKey(policy, webpage)

	// Check if the key exists in the storage
	filePath, err := s.store.Get(key)
	if err == nil {
		// If the key exists, read the content from the file at the stored file path
		contentBytes, err := ioutil.ReadFile(filePath) // Assuming filePath is a list of strings and we need the first path
		if err == nil {
			content := strings.Split(string(contentBytes), "\n") // Parse content as list of strings
			response := map[string]string{
				"Response": strings.Join(content, "\n"),
			}
			if err := json.NewEncoder(w).Encode(response); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// w.Write([]byte(strings.Join(content, "\n")))         // Save and return the content
			return
		}
	}

	policyContent, err := fetchContent(policy)
	if err != nil {
		http.Error(w, "Invalid policy URL", http.StatusBadRequest)
		return
	}
	webpageContent, err := fetchContent(webpage)
	if err != nil {
		http.Error(w, "Invalid webpage URL", http.StatusBadRequest)
		return
	}
	findings, err := s.analyzeContent(policyContent, webpageContent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hashedKey := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))[:10]
	file := fmt.Sprintf("asset/%s.txt", hashedKey)
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		http.Error(w, "Failed to create directory for findings", http.StatusInternalServerError)
		return
	}
	// Create the file only if it doesn't exist
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			f, err = os.OpenFile(file, os.O_WRONLY, 0644)
			if err != nil {
				http.Error(w, "Failed to open existing file", http.StatusInternalServerError)
				return
			}
			// File already exists, no need to create it
		} else {
			// Some other error occurred
			http.Error(w, "Failed to create file", http.StatusInternalServerError)
			return
		}
	}
	defer f.Close()
	if _, err := f.Write([]byte(strings.Join(findings, " "))); err != nil {
		http.Error(w, "Failed to save findings", http.StatusInternalServerError)
		return
	}

	s.store.Insert(key, file)
	response := map[string]string{
		"Response": strings.Join(findings, " "),
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func splitIntoChunks(s string, chunkSize int) []string {
	var chunks []string
	for len(s) > chunkSize {
		chunks = append(chunks, s[:chunkSize])
		s = s[chunkSize:]
	}
	chunks = append(chunks, s)

	return chunks
}

func shouldRetry(err error) bool {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		if httpErr, ok := err.(*openai.APIError); ok {
			if httpErr.HTTPStatusCode == http.StatusInternalServerError ||
				httpErr.HTTPStatusCode == http.StatusBadGateway ||
				httpErr.HTTPStatusCode == http.StatusServiceUnavailable ||
				httpErr.HTTPStatusCode == http.StatusGatewayTimeout {
				return true
			}
		}
	}

	return false
}

func (s *APIServer) analyzeContent(policy, webpage []string) ([]string, error) {
	return s.analyzeContentWithRetry(policy, webpage, 3)
}

func (s *APIServer) analyzeContentWithRetry(policy, webpage []string, retryCount int) ([]string, error) {
	// Initialize the OpenAI client
	client := s.aiClient
	prompt := fmt.Sprintf(s.config.Prompt, webpage, policy)

	dialogue := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: prompt},
	}
	tokens := s.config.OpenAI.Tokens

	// Send the prompt to the GPT-4 model
	// TODO: prompt messgae can be too big due to page content so I think we should create chunk of target page and and check with each chunk of policy else for pig pages it can fails
	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:     openai.GPT4,
			MaxTokens: tokens,
			Messages:  dialogue,

		})
	if err != nil {
		if shouldRetry(err) && retryCount > 0 {
			return s.analyzeContentWithRetry(policy, webpage, retryCount-1)
		}
		return nil, err
	}

	// Extract the findings from the model's output
	findings := strings.Fields(resp.Choices[0].Message.Content)

	return findings, nil
}

func fetchContent(url string) ([]string, error) {
	// Fetch the URL
	resp, err := http.Get(url)
	if err != nil {
		return []string{}, nil
	}
	defer resp.Body.Close()

	// Parse the page body
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return []string{}, nil
	}

	// Use maps to track unique entries
	uniqueHeadings := make(map[string]bool)
	uniqueParagraphs := make(map[string]bool)
	uniqueLists := make(map[string]bool)
	uniqueTables := make(map[string]bool)

	var output []string

	// Extract all elements in the order they appear
	doc.Find("*").Each(func(i int, s *goquery.Selection) {
		switch goquery.NodeName(s) {
		case "h2", "h3":
			headingText := strings.TrimSpace(s.Text())
			if _, exists := uniqueHeadings[headingText]; !exists {
				output = append(output, headingText)
				uniqueHeadings[headingText] = true
			}
		case "p":
			paragraphText := strings.TrimSpace(s.Text())
			if _, exists := uniqueParagraphs[paragraphText]; !exists {
				output = append(output, paragraphText)
				uniqueParagraphs[paragraphText] = true
			}
		case "ul":
			listItems := s.Find("li").Map(func(i int, s *goquery.Selection) string {
				return strings.TrimSpace(s.Text())
			})
			listText := strings.Join(listItems, "\n")
			if _, exists := uniqueLists[listText]; !exists {
				output = append(output, listText)
				uniqueLists[listText] = true
			}
		case "table":
			tableRows := s.Find("tr").Map(func(i int, s *goquery.Selection) string {
				columns := s.Find("th, td").Map(func(i int, s *goquery.Selection) string {
					return strings.TrimSpace(s.Text())
				})
				return strings.Join(columns, "\t")
			})
			tableText := strings.Join(tableRows, "\n")
			if _, exists := uniqueTables[tableText]; !exists {
				output = append(output, tableText)
				uniqueTables[tableText] = true
			}
		}
	})

	return output, nil
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Before request processing")
		next.ServeHTTP(w, r)
		fmt.Println("After request processing")
	})
}

func main() {
	storage := NewMemoryStorage()
	config, _ := ReadConfig()
	openAiClient := openai.NewClient(os.Getenv("OPENAPI_KEY"))
	listenAddr := fmt.Sprintf(":%d", config.Port)
	server := NewAPIServer(listenAddr, storage, openAiClient, config)
	server.Run()
}
