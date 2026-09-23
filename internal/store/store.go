package store

import (
	"crypto/sha256"
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

	"github.com/zongqichen/cfs/internal/contextname"
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
	namedContextPrefix   = "cfs:named-context:v1\x00"
)

type Store struct {
	Root string
	Now  func() time.Time
}

type Context struct {
	ID           string
	Name         string
	Dir          string
	CFHome       string
	LockPath     string
	MetadataPath string
}

type Metadata struct {
	Version     int       `json:"version"`
	ContextID   string    `json:"context_id"`
	ContextName string    `json:"context_name,omitempty"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
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
	return s.ContextForName(ws, contextname.Default)
}

func (s Store) ContextForName(ws workspace.Workspace, name string) (Context, error) {
	if err := contextname.Validate(name); err != nil {
		return Context{}, err
	}
	id := expectedContextID(ws.ID, name)
	ctx, err := s.Context(id)
	if err != nil {
		return Context{}, err
	}
	ctx.Name = name
	return ctx, nil
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
	if !s.hasExpectedPaths(ctx) {
		return errors.New("context paths do not match the state root")
	}
	if err := s.PrepareRoot(); err != nil {
		return err
	}
	for _, dir := range []string{ctx.Dir, ctx.CFHome} {
		if err := securefs.EnsureDirectory(dir); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) PrepareRoot() error {
	for _, dir := range []string{s.Root, filepath.Join(s.Root, contextsDirectory), filepath.Join(s.Root, locksDirectory), s.SharedPluginHome()} {
		if err := securefs.EnsureDirectory(dir); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) Ensure(ctx Context, ws workspace.Workspace) error {
	if err := contextname.Validate(ctx.Name); err != nil {
		return err
	}
	expected, err := s.ContextForName(ws, ctx.Name)
	if err != nil {
		return err
	}
	if !sameContextPaths(ctx, expected) {
		return errors.New("context does not match its workspace and name")
	}
	if err := s.Prepare(ctx); err != nil {
		return err
	}

	metadata, err := s.ReadMetadata(ctx)
	if errors.Is(err, os.ErrNotExist) {
		now := s.now().UTC()
		metadata = Metadata{
			Version:     metadataVersion,
			ContextID:   ctx.ID,
			ContextName: ctx.Name,
			WorkspaceID: ws.ID,
			Workspace:   ws.Root,
			Source:      ws.Source,
			Fingerprint: ws.Fingerprint,
			CreatedAt:   now,
			LastUsedAt:  now,
		}
	} else if err != nil {
		return err
	} else {
		if !metadataMatches(metadata, ctx, ws) {
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
	if metadata.ContextName == "" {
		metadata.ContextName = contextname.Default
	}
	if err := contextname.Validate(metadata.ContextName); err != nil {
		return Metadata{}, err
	}
	if metadata.WorkspaceID == "" && metadata.ContextName == contextname.Default {
		metadata.WorkspaceID = metadata.ContextID
	}
	if !validID(metadata.WorkspaceID) {
		return Metadata{}, errors.New("invalid workspace ID in context metadata")
	}
	if metadata.ContextID != ctx.ID {
		return Metadata{}, errors.New("context metadata does not match its directory")
	}
	if expectedContextID(metadata.WorkspaceID, metadata.ContextName) != metadata.ContextID {
		return Metadata{}, errors.New("context metadata name does not match its ID")
	}
	return metadata, nil
}

func (s Store) ValidateContext(ctx Context) (Metadata, error) {
	if err := s.validateContextDirectories(ctx); err != nil {
		return Metadata{}, err
	}
	return s.ReadMetadata(ctx)
}

func (s Store) ValidateForWorkspace(ctx Context, ws workspace.Workspace) (Metadata, error) {
	metadata, err := s.ValidateContext(ctx)
	if err != nil {
		return Metadata{}, err
	}
	if !metadataMatches(metadata, ctx, ws) {
		return Metadata{}, errors.New("workspace metadata does not match the resolved context")
	}
	return metadata, nil
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
		ctx.Name = metadata.ContextName
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

func (s Store) ListForWorkspace(ws workspace.Workspace) ([]Entry, error) {
	entries, err := s.List()
	if err != nil {
		return nil, err
	}
	matching := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		metadata := entry.Metadata
		if metadata.WorkspaceID == ws.ID && workspace.SameRoot(metadata.Workspace, ws.Root) && metadata.Fingerprint == ws.Fingerprint {
			matching = append(matching, entry)
		}
	}
	return matching, nil
}

func (s Store) MoveToTrash(ctx Context) (string, error) {
	if _, err := s.ValidateContext(ctx); err != nil {
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
	if !s.hasExpectedPaths(ctx) {
		return errors.New("context paths do not match the state root")
	}
	for _, directory := range []string{s.Root, filepath.Join(s.Root, contextsDirectory), ctx.Dir, ctx.CFHome} {
		if err := securefs.ValidateDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) hasExpectedPaths(ctx Context) bool {
	expected, err := s.Context(ctx.ID)
	return err == nil && sameContextPaths(ctx, expected)
}

func sameContextPaths(first, second Context) bool {
	return first.ID == second.ID && first.Dir == second.Dir && first.CFHome == second.CFHome && first.LockPath == second.LockPath && first.MetadataPath == second.MetadataPath
}

func expectedContextID(workspaceID, name string) string {
	if name == contextname.Default {
		return workspaceID
	}
	sum := sha256.Sum256([]byte(namedContextPrefix + workspaceID + "\x00" + name))
	return hex.EncodeToString(sum[:])
}

func metadataMatches(metadata Metadata, ctx Context, ws workspace.Workspace) bool {
	return metadata.ContextID == ctx.ID && metadata.ContextName == ctx.Name && metadata.WorkspaceID == ws.ID && workspace.SameRoot(metadata.Workspace, ws.Root) && metadata.Fingerprint == ws.Fingerprint
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
