package main

import (
	"crypto/sha1"
	"database/sql"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	fmt.Println("vim-go")
}

func fileHandler(db *sql.DB, objects_path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

	}
}

type File struct {
	ID        int64     `json:"id"`
	ObjectID  string    `json:"object_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Tags      []string  `json:"tags"`
	Type      string    `json:"type"`
	// Content holds either the content of the file if text or the link to the file if it is an image
	Content string `json:"content"`
}

func updateFile(db *sql.DB, rootDir string, id int64, r io.ReadSeeker) error {
	hash, err := writeObject(rootDir, r)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	_, err = tx.Exec(`insert into log (file_id, object_id, updated_at) values (?, ?, ?)`, id, hash, now)
	if err != nil {
		return fmt.Errorf("inserting to log: %w", err)
	}

	_, err = tx.Exec(`update files set object_id=?, updated_at=? where id=?`,
		hash, now, id)
	if err != nil {
		return fmt.Errorf("inserting into table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		// no need to delete the file, if the person tries to recreate the file, nothing happens
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func createFile(db *sql.DB, rootDir string, r io.ReadSeeker) (int64, error) {
	hash, err := writeObject(rootDir, r)
	if err != nil {
		return 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	res, err := tx.Exec(`insert into files (object_id, created_at, updated_at) values (?, ?, ?)`,
		hash, now, now)
	if err != nil {
		return 0, fmt.Errorf("inserting into table: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("fetching last id: %w", err)
	}
	res, err = tx.Exec(`insert into log (file_id, object_id, updated_at) values (?, ?, ?)`,
		id, hash, now)
	if err != nil {
		return 0, fmt.Errorf("inserting to log: %w", err)
	}
	if err := tx.Commit(); err != nil {
		// no need to delete the file, if the person tries to recreate the file, nothing happens
		return 0, fmt.Errorf("commit: %w", err)
	}
	return id, nil
}

func writeObject(rootDir string, r io.ReadSeeker) (string, error) {
	h := sha1.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("creating hash: %w", err)
	}
	hash := fmt.Sprintf("%x", h.Sum(nil))
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("error seeking to begin: %w", err)
	}

	objectPath := getObjectPath(rootDir, hash)
	if err := os.MkdirAll(filepath.Dir(objectPath), os.ModePerm); err != nil {
		return "", fmt.Errorf("create object dir: %w", err)
	}

	w, err := os.OpenFile(objectPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("error creating object file: %w", err)
	}
	defer w.Close()
	if _, err := io.Copy(w, r); err != nil {
		return "", fmt.Errorf("error writing object: %w", err)
	}
	return hash, nil
}

func getFile(db *sql.DB, rootDir string, id int64) (File, error) {
	stmt := `SELECT object_id, created_at, updated_at from files where id=?`
	var objectID string
	var createdAt, updatedAt int64
	err := db.QueryRow(stmt, id).Scan(&objectID, &createdAt, &updatedAt)
	if err != nil {
		return File{}, fmt.Errorf("could query row: %w", err)
	}
	f := File{
		ID:        id,
		ObjectID:  objectID,
		CreatedAt: time.Unix(createdAt, 0),
		UpdatedAt: time.Unix(updatedAt, 0),
	}
	b, err := os.Open(getObjectPath(rootDir, objectID))
	if err != nil {
		return File{}, err
	}
	defer b.Close()
	fileType, err := fileContentType(b)
	if err != nil {
		return File{}, err
	}
	if _, err := b.Seek(0, io.SeekStart); err != nil {
		return File{}, fmt.Errorf("error seeking to begin: %w", err)
	}
	// len of text/plain==10
	if fileType[:10] == "text/plain" {
		raw, err := ioutil.ReadAll(b)
		if err != nil {
			return File{}, err
		}
		f.Content = string(raw)
	}
	f.Type = fileType
	return f, nil
}

func fileContentType(in io.Reader) (string, error) {
	// Only the first 512 bytes are used to sniff the content type.
	raw, err := ioutil.ReadAll(&(io.LimitedReader{R: in, N: 512}))
	if err != nil {
		return "", err
	}
	return http.DetectContentType(raw), nil
}

func getObjectPath(rootDir, hash string) string {
	dir := hash[:2]
	file := hash[2:]
	return filepath.Join(rootDir, "objects", dir, file)
}
