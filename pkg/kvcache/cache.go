// Package kvcache 提供一个泛型、并发安全的内存 KV 缓存，基于 sync.Map 封装。
package kvcache

import "sync"

// Cache - Provides simple mechanism to hold any key value in memory
// wrapped around via sync.Map but typed with generics.
type Cache[K comparable, V any] struct {
	m sync.Map
}

// Delete delete the key
func (r *Cache[K, V]) Delete(key K) {
	r.m.Delete(key)
}

// Get - Returns a value of a given key if it exists.
func (r *Cache[K, V]) Get(key K) (value V, ok bool) {
	v, ok := r.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}

// Set - Will persist a value into cache.
func (r *Cache[K, V]) Set(key K, value V) {
	r.m.Store(key, value)
}
