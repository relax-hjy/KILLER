// Package cache 简单的带 TTL 内存缓存。
//
// 用于对东财等免费接口做请求节流：同一 key 在 TTL 内只打一次上游，
// 避免前端轮询放大成对数据源的高频请求（曾触发 IP 限流）。
package cache

import (
	"sync"
	"time"
)

type entry struct {
	val any
	exp  time.Time
}

// Cache 泛型 TTL 缓存。
type Cache struct {
	mu sync.Mutex
	m  map[string]entry
}

// New 创建空缓存。
func New() *Cache {
	return &Cache{m: make(map[string]entry)}
}

// GetOrSet 缓存命中返回缓存值，否则调用 fn 并按 ttl 写入。
// fn 的错误不缓存（下次调用重试）。
func (c *Cache) GetOrSet(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	c.mu.Lock()
	if e, ok := c.m[key]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.val, nil
	}
	c.mu.Unlock()

	val, err := fn()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.m[key] = entry{val: val, exp: time.Now().Add(ttl)}
	c.mu.Unlock()
	return val, nil
}
