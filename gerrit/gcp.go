package gerrit

import (
	"context"

	"cloud.google.com/go/datastore"
	"google.golang.org/api/iterator"
)

// GCPStore implements Store and tracks state in a Google Cloud Platform Datastore.
type GCPStore struct {
	ds   *datastore.Client
	kind string
}

// NewGCPStore construct a new *GCPStore.
func NewGCPStore(ds *datastore.Client) *GCPStore {
	return &GCPStore{
		ds:   ds,
		kind: "GoCL",
	}
}

func (s *GCPStore) LatestNumber(ctx context.Context) (int, error) {
	q := datastore.NewQuery(s.kind).
		Order("-CrawledAt").
		Limit(1).
		KeysOnly()

	key, err := s.ds.Run(ctx, q).Next(nil)
	if err == iterator.Done {
		return 0, ErrNotFound
	}
	return int(key.ID), err
}

func (s *GCPStore) Put(ctx context.Context, number int, cl storedCL) error {
	_, err := s.ds.Put(ctx, s.key(number), cl)
	return err
}

func (s *GCPStore) Exists(ctx context.Context, number int) (bool, error) {
	var cl storedCL
	err := s.ds.Get(ctx, s.key(number), &cl)
	if err == datastore.ErrNoSuchEntity {
		return false, err
	}
	return true, err
}

func (s *GCPStore) key(clNumber int) *datastore.Key {
	return datastore.IDKey(s.kind, int64(clNumber), nil)
}
