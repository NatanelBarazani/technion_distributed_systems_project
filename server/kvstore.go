package server

import (
	"encoding/json"
	"sort"
	"sync"
)

// KVStore is a simple in-memory key–value store.
type KVStore struct {
	mu   sync.Mutex
	data map[string]string
}

// NewKVStore creates a new KVStore.
func NewKVStore() *KVStore {
	return &KVStore{
		data: make(map[string]string),
	}
}

// Get returns the value for a key.
func (kv *KVStore) Get(key string) (string, bool) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	val, ok := kv.data[key]
	return val, ok
}

// Set sets the value for a key.
func (kv *KVStore) Set(key, value string) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	kv.data[key] = value
}

// ListKeys returns a sorted list of all keys.
func (kv *KVStore) ListKeys() []string {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	keys := make([]string, 0, len(kv.data))
	for k := range kv.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// GetRange returns key–value pairs for keys between start and end (inclusive).
func (kv *KVStore) GetRange(start, end string) map[string]string {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	result := make(map[string]string)
	for k, v := range kv.data {
		if k >= start && k <= end {
			result[k] = v
		}
	}
	return result
}

// Snapshot serializes the KVStore data (for log compaction).
func (kv *KVStore) Snapshot() ([]byte, error) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	return json.Marshal(kv.data)
}
