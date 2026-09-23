package store

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	metadataReadLimit    = 64 * 1024
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
	return s.Context(ws.ID)
}

func (s Store) Context(id string) (Context, error) {
	if !validID(id) {
		return Context{}, errors.New("invalid context ID")
	}
	contextDir := filepath.Join(s.Root, contextsDirectory, id)
	return Context{
		ID:           id,
		Dir:          contextDir,
		CFHome:       filepath.Join(contextDir, contextHomeDirectory),
		LockPath:     filepath.Join(s.Root, locksDirectory, id+lockFileSuffix),
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
		if metadata.ContextID != ws.ID || !workspace.SameRoot(metadata.Workspace, ws.Root) || metadata.Fingerprint != ws.Fingerprint {
			return errors.New("workspace metadata does not match the resolved context")
		}
		metadata.LastUsedAt = s.now().UTC()
	}

	return securefs.WriteJSONAtomic(ctx.MetadataPath, metadata)
}

func (s Store) ReadMetadata(ctx Context) (Metadata, error) {
	file, err := securefs.OpenPrivateFile(ctx.MetadataPath, os.O_RDONLY)
	if err != nil {
		return Metadata{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, metadataReadLimit+1))
	closeErr := file.Close()
	if readErr != nil {
		return Metadata{}, fmt.Errorf("read %s: %w", ctx.MetadataPath, readErr)
	}
	if closeErr != nil {
		return Metadata{}, fmt.Errorf("close %s: %w", ctx.MetadataPath, closeErr)
	}
	if len(raw) > metadataReadLimit {
		return Metadata{}, fmt.Errorf("metadata exceeds %d bytes: %s", metadataReadLimit, ctx.MetadataPath)
	}
	var metadata Metadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("parse %s: %w", ctx.MetadataPath, err)
	}
	if metadata.Version != metadataVersion {
		return Metadata{}, fmt.Errorf("unsupported metadata version %d", metadata.Version)
	}
	if metadata.ContextID != ctx.ID {
		return Metadata{}, errors.New("context metadata does not match its directory")
	}
	return metadata, nil
}

func (s Store) ValidateContext(ctx Context) (Metadata, error) {
	if err := s.validateContextDirectories(ctx); err != nil {
		return Metadata{}, err
	}
	return s.ReadMetadata(ctx)
}

func (s Store) SharedPluginHome() string {
	return filepath.Join(s.Root, pluginsDirectory)
}

func (s Store) List() ([]Entry, error) {
	root := filepath.Join(s.Root, contextsDirectory)
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("inspect contexts: %w", err)
	}
	if err := securefs.ValidateDirectory(s.Root); err != nil {
		return nil, err
	}
	if err := securefs.ValidateDirectory(root); err != nil {
		return nil, err
	}
	directories, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("list contexts: %w", err)
	}

	entries := make([]Entry, 0, len(directories))
	for _, directory := range directories {
		if !directory.IsDir() || !validID(directory.Name()) {
			continue
		}
		ctx, err := s.Context(directory.Name())
		if err != nil {
			continue
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
	if err := s.validateContextDirectories(ctx); err != nil {
		return "", err
	}
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

func (s Store) validateContextDirectories(ctx Context) error {
	expected, err := s.Context(ctx.ID)
	if err != nil {
		return err
	}
	if ctx != expected {
		return errors.New("context paths do not match the state root")
	}
	for _, directory := range []string{s.Root, filepath.Join(s.Root, contextsDirectory), ctx.Dir, ctx.CFHome} {
		if err := securefs.ValidateDirectory(directory); err != nil {
			return err
		}
	}
	return nil
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
