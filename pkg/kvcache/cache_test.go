package kvcache

import "testing"

func TestCacheSetGet(t *testing.T) {
	c := Cache[string, int]{}

	// Get non-existent key
	_, ok := c.Get("missing")
	if ok {
		t.Error("expected false for missing key")
	}

	// Set and Get
	c.Set("foo", 42)
	v, ok := c.Get("foo")
	if !ok {
		t.Fatal("expected true for existing key")
	}
	if v != 42 {
		t.Errorf("got %d, want 42", v)
	}

	// Overwrite
	c.Set("foo", 99)
	v, _ = c.Get("foo")
	if v != 99 {
		t.Errorf("got %d, want 99 after overwrite", v)
	}
}

func TestCacheDelete(t *testing.T) {
	c := Cache[string, string]{}

	c.Set("key", "value")
	_, ok := c.Get("key")
	if !ok {
		t.Fatal("expected key to exist")
	}

	c.Delete("key")
	_, ok = c.Get("key")
	if ok {
		t.Error("expected key to be deleted")
	}
}

func TestCacheMultipleTypes(t *testing.T) {
	// String keys, struct values
	type val struct{ Name string }
	c := Cache[string, val]{}
	c.Set("a", val{Name: "hello"})
	v, ok := c.Get("a")
	if !ok || v.Name != "hello" {
		t.Errorf("got %+v, want {Name:hello}", v)
	}

	// Int keys, string values
	c2 := Cache[int, string]{}
	c2.Set(1, "one")
	s, ok := c2.Get(1)
	if !ok || s != "one" {
		t.Errorf("got %q, want %q", s, "one")
	}
}
