package store

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zongqichen/cfs/internal/securefs"
	"github.com/zongqichen/cfs/internal/workspace"
)

const (
	metadataVersion      = 1
	contextsDirectory    = "contexts"
	locksDirectory       = "locks"
	pluginsDirectory     = "plugins"
	trashDirectory       = "trash"
	contextHomeDirectory = "home"
	metadataFileName     = "metadata.json"
	lockFileSuffix       = ".lock"
	trashTimestampLayout = "20060102T150405.000000000Z"
)

type Store struct {
	Root string
	Now  func() time.Time
}

type Context struct {
	ID           string
	Dir          string
	CFHome       string
	LockPath     string
	MetadataPath string
}

type Metadata struct {
	Version     int       `json:"version"`
	ContextID   string    `json:"context_id"`
	Workspace   string    `json:"workspace"`
	Source      string    `json:"source"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
}

type Entry struct {
	Context  Context
	Metadata Metadata
	Orphaned bool
}

func New(root string) Store {
	return Store{Root: filepath.Clean(root), Now: time.Now}
}

func (s Store) ContextFor(ws workspace.Workspace) (Context, error) {
	if !validID(ws.ID) {
		return Context{}, errors.New("workspace returned an invalid context ID")
	}
	contextDir := filepath.Join(s.Root, contextsDirectory, ws.ID)
	return Context{
		ID:           ws.ID,
		Dir:          contextDir,
		CFHome:       filepath.Join(contextDir, contextHomeDirectory),
		LockPath:     filepath.Join(s.Root, locksDirectory, ws.ID+lockFileSuffix),
		MetadataPath: filepath.Join(contextDir, metadataFileName),
	}, nil
}

func (s Store) Prepare(ctx Context) error {
	for _, dir := range []string{s.Root, filepath.Join(s.Root, contextsDirectory), filepath.Join(s.Root, locksDirectory), ctx.Dir, ctx.CFHome, s.SharedPluginHome()} {
		if err := securefs.EnsureDirectory(dir); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) Ensure(ctx Context, ws workspace.Workspace) error {
	if err := s.Prepare(ctx); err != nil {
		return err
	}

	metadata, err := s.ReadMetadata(ctx)
	if errors.Is(err, os.ErrNotExist) {
		now := s.now().UTC()
		metadata = Metadata{
			Version:     metadataVersion,
			ContextID:   ws.ID,
			Workspace:   ws.Root,
			Source:      ws.Source,
			Fingerprint: ws.Fingerprint,
			CreatedAt:   now,
			LastUsedAt:  now,
		}
	} else if err != nil {
		return err
	} else {
		if metadata.ContextID != ws.ID || metadata.Workspace != ws.Root || metadata.Fingerprint != ws.Fingerprint {
			return errors.New("workspace metadata does not match the resolved context")
		}
		metadata.LastUsedAt = s.now().UTC()
	}

	return securefs.WriteJSONAtomic(ctx.MetadataPath, metadata)
}

func (s Store) ReadMetadata(ctx Context) (Metadata, error) {
	raw, err := os.ReadFile(ctx.MetadataPath)
	if err != nil {
		return Metadata{}, err
	}
	var metadata Metadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("parse %s: %w", ctx.MetadataPath, err)
	}
	if metadata.Version != metadataVersion {
		return Metadata{}, fmt.Errorf("unsupported metadata version %d", metadata.Version)
	}
	return metadata, nil
}

func (s Store) SharedPluginHome() string {
	return filepath.Join(s.Root, pluginsDirectory)
}

func (s Store) List() ([]Entry, error) {
	root := filepath.Join(s.Root, contextsDirectory)
	directories, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list contexts: %w", err)
	}

	entries := make([]Entry, 0, len(directories))
	for _, directory := range directories {
		if !directory.IsDir() || !validID(directory.Name()) {
			continue
		}
		ctx := Context{
			ID:           directory.Name(),
			Dir:          filepath.Join(root, directory.Name()),
			CFHome:       filepath.Join(root, directory.Name(), contextHomeDirectory),
			LockPath:     filepath.Join(s.Root, locksDirectory, directory.Name()+lockFileSuffix),
			MetadataPath: filepath.Join(root, directory.Name(), metadataFileName),
		}
		metadata, err := s.ReadMetadata(ctx)
		if err != nil {
			return nil, err
		}
		_, statErr := os.Stat(metadata.Workspace)
		entries = append(entries, Entry{
			Context:  ctx,
			Metadata: metadata,
			Orphaned: errors.Is(statErr, os.ErrNotExist),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Metadata.LastUsedAt.After(entries[j].Metadata.LastUsedAt)
	})
	return entries, nil
}

func (s Store) MoveToTrash(ctx Context) (string, error) {
	trashRoot := filepath.Join(s.Root, trashDirectory)
	if err := securefs.EnsureDirectory(trashRoot); err != nil {
		return "", err
	}
	timestamp := s.now().UTC().Format(trashTimestampLayout)
	destination := filepath.Join(trashRoot, ctx.ID+"-"+timestamp)
	if err := os.Rename(ctx.Dir, destination); err != nil {
		return "", fmt.Errorf("move context to trash: %w", err)
	}
	return destination, nil
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func validID(id string) bool {
	if len(id) != sha256HexLength {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && !strings.ContainsAny(id, `/\`)
}

const sha256HexLength = 64
