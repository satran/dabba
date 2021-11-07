package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	root   string
	config *Config
}

func NewStore(root string) (*Store, error) {
	// Create the file if it doesn't exist
	f, err := os.OpenFile(filepath.Join(root, "config.json"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("can't open settings file: %w", err)
	}
	defer f.Close()
	c := &Config{}
	if err := json.NewDecoder(f).Decode(c); err != nil {
		return nil, fmt.Errorf("decode settings file: %w", err)
	}
	return &Store{root: root, config: c}, nil
}

type Config struct {
	StartFile string `json:"start-file"`
}

func (s *Store) Get(name string) (*File, error) {
	f, err := os.Open(s.path(name))
	if err != nil {
		return nil, fmt.Errorf("open file %q: %w", name, err)
	}
	meta, err := s.Meta(name)
	if err != nil {
		f.Close()
		return nil, err
	}
	type_, err := fileContentType(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	println(type_)
	file := File{
		ReadWriteSeeker: f,
		Name:            name,
		Meta:            meta,
		Type:            type_,
		path:            s.path(name),
		close:           f.Close,
	}
	if err := file.SeekStart(); err != nil {
		file.Close()
		return nil, err
	}
	return &file, err
}

func (s *Store) Meta(name string) (Meta, error) {
	raw, err := ioutil.ReadFile(s.metaPath(name))
	if err != nil {
		return nil, fmt.Errorf("reading meta file %q: %w", name, err)
	}
	lines := strings.Split(string(raw), "\n")
	m := make(map[string]string)
	for i, line := range lines {
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		chunks := strings.SplitN(line, ":", 2)
		if len(chunks) != 2 {
			return nil, fmt.Errorf("meta file %q corrupted on line %d: %s", name, i, line)
		}
		m[chunks[0]] = chunks[1]
	}
	return Meta(m), nil
}

func (s *Store) Write(name string, r io.Reader, meta Meta) error {
	path := s.path(name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("creating file %q: %w", name, err)
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	if err != nil {
		return fmt.Errorf("write file %q: %w", name, err)
	}
	return s.WriteMeta(name, meta)
}

func (s *Store) WriteMeta(name string, meta Meta) error {
	path := s.metaPath(name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("creating file %q: %w", name, err)
	}
	defer f.Close()
	for key, value := range meta {
		fmt.Fprintf(f, "%s:%s", key, value)
	}
	return nil
}

func (s *Store) Index() string {
	return s.config.StartFile
}

func (s *Store) path(name string) string {
	return filepath.Join(s.root, "objects", name+".dabba")
}

func (s *Store) metaPath(name string) string {
	return filepath.Join(s.root, "meta", name+".meta")
}

type File struct {
	io.ReadWriteSeeker
	Name  string
	Type  string
	Meta  Meta
	path  string
	close func() error
}

func (f *File) Close() error {
	return f.close()
}

func (f *File) IsText() bool {
	return strings.HasPrefix(f.Type, "text/plain")
}

func (f *File) IsImage() bool {
	for _, t := range []string{
		"image/avif",
		"image/gif",
		"image/jpeg",
		"image/jpeg",
		"image/png",
		"image/svg+xml",
		"image/webp",
	} {
		if f.Type == t {
			return true
		}
	}
	return false
}

func (f *File) Content() string {
	content, err := ioutil.ReadAll(f)
	if err != nil {
		// for now return the parser error as content
		return err.Error()
	}
	return string(content)
}

func (f *File) SeekStart() error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("error seeking to begin: %w", err)
	}
	return nil
}

type Meta map[string]string

func fileContentType(r io.ReadSeeker) (string, error) {
	// Only the first 512 bytes are used to sniff the content type.
	raw, err := ioutil.ReadAll(&(io.LimitedReader{R: r, N: 512}))
	if err != nil {
		return "", err
	}
	fileType, _, err := mime.ParseMediaType(http.DetectContentType(raw))
	if err != nil {
		return "", err
	}
	return fileType, nil
}
