package statetest_test

import (
	"bytes"
	"context"
	"sync"
	"time"
)

// mapBytes is the smallest BytesStore: a map, for exercising brick.Bytes.
type mapBytes struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMapBytes() *mapBytes { return &mapBytes{m: map[string][]byte{}} }

func (s *mapBytes) Load(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return bytes.Clone(b), ok, nil
}

func (s *mapBytes) Save(_ context.Context, key string, data []byte, _ time.Duration, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = bytes.Clone(data)
	return nil
}

func (s *mapBytes) Remove(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *mapBytes) EachBytes(_ context.Context, fn func(key string, data []byte) bool) error {
	s.mu.Lock()
	all := map[string][]byte{}
	for k, v := range s.m {
		all[k] = bytes.Clone(v)
	}
	s.mu.Unlock()
	for k, v := range all {
		if !fn(k, v) {
			break
		}
	}
	return nil
}
