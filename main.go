package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

const (
	dbConnStr = "host=db user=user password=password dbname=filedb sslmode=disable"
)

type FileMetadata struct {
	ID        int    `json:"id"`
	FileName  string `json:"file_name"`
	Bucket1ID string `json:"bucket1_id"`
	Bucket2ID string `json:"bucket2_id"`
	Bucket3ID string `json:"bucket3_id"`
}
type Chunk struct {
	id   string
	data []byte
}

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("Error opening database connection: %v", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		log.Fatalf("Error connecting to the database: %v", err)
	}

	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/files", getFilesHandler)
	http.HandleFunc("/download/", downloadHandler)

	log.Println("Server starting on port 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	/*
		upload files in parallel
		create the metadata first, save it in postgres
		and then write the files to harddisk in parallel to individual buckets
	*/
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		log.Printf("Error getting form file: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read the entire file
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		log.Printf("Error reading file: %v", err)
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	fileSize := len(fileBytes)
	chunkSize := int(math.Ceil(float64(fileSize) / 3))

	bucket1ID := generateUniqueID()
	bucket2ID := generateUniqueID()
	bucket3ID := generateUniqueID()

	chunks := []Chunk{
		{bucket1ID, fileBytes[:chunkSize]},
		{bucket2ID, fileBytes[chunkSize : 2*chunkSize]},
		{bucket3ID, fileBytes[2*chunkSize:]},
	}

	errChan := make(chan error, len(chunks))
	done := make(chan int, len(chunks))

	for index, chunk := range chunks {
		go func(i int, chunk Chunk) {
			if err := writeChunk(i, chunk); err != nil {
				errChan <- err
			} else {
				done <- 1
			}

		}(index, chunk)
	}

	successCount := 0

Loop:
	for {
		select {
		case <-errChan:
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		case <-done:
			successCount++
			if successCount == len(chunks) {
				break Loop
			}
		}
	}

	var fileID int
	err = db.QueryRow("INSERT INTO files (original_filename, bucket1_id, bucket2_id, bucket3_id) VALUES ($1, $2, $3, $4) RETURNING id",
		header.Filename, bucket1ID, bucket2ID, bucket3ID).Scan(&fileID)
	if err != nil {
		log.Printf("Error inserting file metadata: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("File %s uploaded successfully with ID %d", header.Filename, fileID)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File uploaded successfully. ID: %d", fileID)
}

func writeChunk(i int, chunk Chunk) (err error) {
	chunkPath := fmt.Sprintf("/tmp/bucket%d/%s", i+1, chunk.id)
	err = os.MkdirAll(filepath.Dir(chunkPath), 0755)
	if err != nil {
		log.Printf("Error creating directory for chunk %d: %v", i+1, err)
		return
	}

	err = os.WriteFile(chunkPath, chunk.data, 0644)
	if err != nil {
		log.Printf("Error writing chunk %d: %v", i+1, err)
		return
	}
	log.Printf("Chunk %d saved to %s", i+1, chunkPath)
	return
}

func getFilesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query("SELECT id, original_filename, bucket1_id, bucket2_id, bucket3_id FROM files")
	if err != nil {
		log.Printf("Error querying files: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var files []FileMetadata
	for rows.Next() {
		var file FileMetadata
		err := rows.Scan(&file.ID, &file.FileName, &file.Bucket1ID, &file.Bucket2ID, &file.Bucket3ID)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		files = append(files, file)
	}

	jsonResponse, err := json.Marshal(files)
	if err != nil {
		log.Printf("Error marshalling JSON: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
	/*
		since chunks need to be written in order
		we can query them in batches and then write them one by one
		loading all chunk metadata in memory at once by querying the db
		and then write them back in sequence
	*/
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileID := filepath.Base(r.URL.Path)

	var file FileMetadata
	err := db.QueryRow("SELECT id, original_filename, bucket1_id, bucket2_id, bucket3_id FROM files WHERE id = $1", fileID).
		Scan(&file.ID, &file.FileName, &file.Bucket1ID, &file.Bucket2ID, &file.Bucket3ID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			log.Printf("Database error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", file.FileName))
	w.Header().Set("Content-Type", "application/octet-stream")

	chunks := []string{
		"/tmp/bucket1/" + file.Bucket1ID,
		"/tmp/bucket2/" + file.Bucket2ID,
		"/tmp/bucket3/" + file.Bucket3ID,
	}

	for _, chunkPath := range chunks {
		chunkData, err := os.ReadFile(chunkPath)
		if err != nil {
			log.Printf("Error reading chunk %s: %v", chunkPath, err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		_, err = w.Write(chunkData)
		if err != nil {
			log.Printf("Error writing chunk %s: %v", chunkPath, err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	log.Printf("File %s (ID: %d) downloaded successfully", file.FileName, file.ID)
}

func generateUniqueID() string {
	return uuid.New().String()
}
