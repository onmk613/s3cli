package kvcache

import (
	"sync"
	"testing"
)

func TestCacheLifecycleAndConcurrentAccess(t *testing.T) {
	var c Cache[string, int]
	if _, ok := c.Get("missing"); ok {
		t.Fatal("missing key unexpectedly found")
	}
	c.Set("one", 1)
	if got, ok := c.Get("one"); !ok || got != 1 {
		t.Fatalf("Get = (%d,%v)", got, ok)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); c.Set("key", i); _, _ = c.Get("key") }(i)
	}
	wg.Wait()
	c.Delete("one")
	if _, ok := c.Get("one"); ok {
		t.Fatal("deleted key unexpectedly found")
	}
}
