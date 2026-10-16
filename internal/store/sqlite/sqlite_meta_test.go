package sqlite

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/docker-secret-operator/dso/internal/store"
)

func TestMeta_PutIfAbsentIsAllOrNothing(t *testing.T) {
	s, _ := open(t)
	e := []store.MetaEntry{{Key: "binding.a", Value: []byte("1")}, {Key: "binding.b", Value: []byte("2")}}
	if ok, err := s.PutMetaIfAbsent(ctx, e); err != nil || !ok {
		t.Fatalf("first put: %v %v", ok, err)
	}
	// a second initialiser changes nothing, even with different values
	if ok, err := s.PutMetaIfAbsent(ctx, []store.MetaEntry{{Key: "binding.a", Value: []byte("X")}, {Key: "binding.b", Value: []byte("Y")}}); err != nil || ok {
		t.Fatalf("second put: %v %v", ok, err)
	}
	if v, _ := s.GetMeta(ctx, "binding.a"); string(v) != "1" {
		t.Fatalf("existing value overwritten: %q", v)
	}
	// one existing key blocks the whole set: no partial insert
	if ok, err := s.PutMetaIfAbsent(ctx, []store.MetaEntry{{Key: "binding.c", Value: []byte("3")}, {Key: "binding.a", Value: []byte("Z")}}); err != nil || ok {
		t.Fatalf("partial put: %v %v", ok, err)
	}
	if _, err := s.GetMeta(ctx, "binding.c"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("partial insert leaked key c: %v", err)
	}
	if _, err := s.PutMetaIfAbsent(ctx, nil); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("empty put: %v", err)
	}
	got, err := s.ListMeta(ctx, "binding.")
	if err != nil || len(got) != 2 || got[0].Key != "binding.a" {
		t.Fatalf("list: %+v %v", got, err)
	}
	if got, _ := s.ListMeta(ctx, "other."); len(got) != 0 {
		t.Fatalf("prefix leaked: %+v", got)
	}
}

func TestMeta_RowsAreImmutableThroughTheDatabase(t *testing.T) {
	s, _ := open(t)
	s.PutMetaIfAbsent(ctx, []store.MetaEntry{{Key: "k", Value: []byte("v")}})
	if _, err := s.db.Exec(`UPDATE store_meta SET value = x'00' WHERE key = 'k'`); err == nil {
		t.Fatal("metadata updated")
	}
	if _, err := s.db.Exec(`DELETE FROM store_meta WHERE key = 'k'`); err == nil {
		t.Fatal("metadata deleted")
	}
}

func TestMeta_ConcurrentInitialisersHaveExactlyOneWinner(t *testing.T) {
	s, path := open(t)
	_ = s
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h, err := Open(ctx, path, Options{})
			if err != nil {
				t.Error(err)
				return
			}
			defer h.Close()
			ok, err := h.PutMetaIfAbsent(ctx, []store.MetaEntry{{Key: "binding.x", Value: []byte{byte(i)}}, {Key: "binding.y", Value: []byte{byte(i)}}})
			if err != nil {
				t.Error(err)
			}
			if ok {
				atomic.AddInt32(&wins, 1)
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d winners", wins)
	}
	x, _ := s.GetMeta(ctx, "binding.x")
	y, _ := s.GetMeta(ctx, "binding.y")
	if len(x) != 1 || len(y) != 1 || x[0] != y[0] {
		t.Fatalf("rows come from different initialisers: %v %v", x, y)
	}
}
