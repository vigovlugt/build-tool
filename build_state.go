package main

import (
	"path/filepath"
)

type BuildState struct {
	localCache *LocalCache
	stampCache *FileStampCache
}

func NewBuildState(cacheRoot string, stampCachePath string) *BuildState {
	return &BuildState{
		localCache: NewLocalCache(cacheRoot),
		stampCache: NewFileStampCache(stampCachePath),
	}
}

func (s *BuildState) Load() error {
	return s.stampCache.Load()
}

func (s *BuildState) Save() error {
	return s.stampCache.Save()
}

func (s *BuildState) ComputeKey(task Task, depKeys []string) (string, []byte, error) {
	return ComputeTaskKey(task, depKeys, s.stampCache)
}

func (s *BuildState) Restore(taskKey string, outputs []Path) (bool, error) {
	return s.localCache.Restore(taskKey, outputs)
}

func (s *BuildState) Store(taskKey string, taskJSON []byte, outputs []Path) error {
	return s.localCache.Store(taskKey, taskJSON, outputs)
}

// UpdateOutputStamps hashes output files and records their stamps so that
// downstream tasks (which may consume these outputs as inputs) get stamp cache
// hits instead of re-hashing.
func (s *BuildState) UpdateOutputStamps(outputs []Path) {
	for _, out := range outputs {
		p := filepath.FromSlash(string(out))
		d, err := hashFile(p)
		if err != nil {
			continue
		}
		s.stampCache.Update(p, d)
	}
}
