package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type scoreEntry struct {
	Name      string `json:"name"`
	Score     int    `json:"score"`
	Timestamp string `json:"timestamp"`
}

type scoreInput struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

var (
	scoreFile = filepath.Join("data", "scores.csv")
	scores    []scoreEntry
	mu        sync.Mutex
)

func main() {
	if err := loadScoresFromDisk(); err != nil {
		log.Printf("could not load score file: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", healthHandler)
	mux.HandleFunc("/api/scores", scoresHandler)

	port := os.Getenv("PORT")
	if strings.TrimSpace(port) == "" {
		port = "8080"
	}

	addr := ":" + port
	log.Printf("Pacman backend listening on port %s", port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		sendNoContent(w)
		return
	}

	if r.Method != http.MethodGet {
		sendJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	sendJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func scoresHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		sendNoContent(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		mu.Lock()
		clone := append([]scoreEntry(nil), scores...)
		mu.Unlock()
		sendJSON(w, http.StatusOK, clone)
	case http.MethodPost:
		handlePostScore(w, r)
	default:
		sendJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}
}

func handlePostScore(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var input scoreInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}

	if input.Score < 0 {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid score"})
		return
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "PLAYER"
	}
	if len(name) > 20 {
		name = name[:20]
	}

	entry := scoreEntry{
		Name:      name,
		Score:     input.Score,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	mu.Lock()
	scores = append(scores, entry)
	sortAndTrimScoresLocked()
	persistErr := persistScoresLocked()
	mu.Unlock()

	if persistErr != nil {
		log.Printf("could not persist score file: %v", persistErr)
	}

	sendJSON(w, http.StatusCreated, map[string]bool{"saved": true})
}

func loadScoresFromDisk() error {
	if err := os.MkdirAll(filepath.Dir(scoreFile), 0o755); err != nil {
		return err
	}

	f, err := os.Open(scoreFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	for _, record := range records {
		if len(record) < 3 {
			continue
		}

		scoreValue, convErr := strconv.Atoi(strings.TrimSpace(record[1]))
		if convErr != nil {
			continue
		}

		scores = append(scores, scoreEntry{
			Name:      strings.TrimSpace(record[0]),
			Score:     scoreValue,
			Timestamp: strings.TrimSpace(record[2]),
		})
	}

	sortAndTrimScoresLocked()
	return nil
}

func persistScoresLocked() error {
	if err := os.MkdirAll(filepath.Dir(scoreFile), 0o755); err != nil {
		return err
	}

	f, err := os.Create(scoreFile)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	for _, score := range scores {
		record := []string{score.Name, strconv.Itoa(score.Score), score.Timestamp}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func sortAndTrimScoresLocked() {
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})
	if len(scores) > 10 {
		scores = scores[:10]
	}
}

func sendNoContent(w http.ResponseWriter) {
	addCorsHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func sendJSON(w http.ResponseWriter, statusCode int, payload any) {
	addCorsHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func addCorsHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
