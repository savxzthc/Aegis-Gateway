package uploads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var validID = regexp.MustCompile(`^upl_[A-Za-z0-9_-]+$`)

type Metadata struct {
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	OwnerID     string    `json:"owner_id,omitempty"`
}

type Store struct {
	dir string
}

func NewStore(dir string) (*Store, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: absolute}, nil
}

func (s *Store) Save(ctx context.Context, id, contentType string, data []byte) error {
	return s.SaveOwned(ctx, id, contentType, "", data)
}

func (s *Store) SaveOwned(ctx context.Context, id, contentType, ownerID string, data []byte) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	meta := Metadata{ContentType: contentType, Size: int64(len(data)), CreatedAt: time.Now().UTC(), OwnerID: ownerID}
	encoded, err := json.Marshal(meta)
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := os.WriteFile(path+".meta", encoded, 0o600); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func (s *Store) Load(ctx context.Context, id string) ([]byte, string, error) {
	path, err := s.path(id)
	if err != nil {
		return nil, "", err
	}
	meta, err := s.Metadata(ctx, id)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	return data, meta.ContentType, nil
}

func (s *Store) Metadata(ctx context.Context, id string) (Metadata, error) {
	path, err := s.path(id)
	if err != nil {
		return Metadata{}, err
	}
	select {
	case <-ctx.Done():
		return Metadata{}, ctx.Err()
	default:
	}
	data, err := os.ReadFile(path + ".meta")
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	path, err := s.path(id)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	dataErr := os.Remove(path)
	metaErr := os.Remove(path + ".meta")
	if errors.Is(dataErr, os.ErrNotExist) && errors.Is(metaErr, os.ErrNotExist) {
		return os.ErrNotExist
	}
	return errors.Join(ignoreNotExist(dataErr), ignoreNotExist(metaErr))
}

func (s *Store) Prune(ctx context.Context, maxAge time.Duration) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-maxAge)
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) == ".meta" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(s.dir, entry.Name()))
			_ = os.Remove(filepath.Join(s.dir, entry.Name()+".meta"))
		}
	}
	return nil
}

func (s *Store) path(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid upload id")
	}
	path := filepath.Join(s.dir, id)
	if filepath.Dir(path) != s.dir {
		return "", fmt.Errorf("upload path escapes store")
	}
	return path, nil
}

func ignoreNotExist(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
