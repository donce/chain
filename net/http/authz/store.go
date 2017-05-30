package authz

import (
	"bytes"
	"context"
	"time"

	"chain/database/sinkdb"
	"chain/errors"
)

// Generate code for the Grant and GrantList types.
//go:generate protoc -I. -I$CHAIN/.. --go_out=. grant.proto

// Store provides persistent storage for grant objects.
type Store struct {
	sdb       *sinkdb.DB
	keyPrefix string
}

// NewStore returns a new *Store storing grants
// in db under keyPrefix.
// It implements the Loader interface.
func NewStore(db *sinkdb.DB, keyPrefix string) *Store {
	return &Store{db, keyPrefix}
}

// Load satisfies the Loader interface.
func (s *Store) Load(ctx context.Context, policy []string) ([]*Grant, error) {
	var grants []*Grant
	for _, p := range policy {
		var grantList GrantList
		found, err := s.sdb.GetStale(s.keyPrefix+p, &grantList)
		if err != nil {
			return nil, err
		} else if found {
			grants = append(grants, grantList.Grants...)
		}
	}
	return grants, nil
}

// Save returns an Op to store g.
// If a grant equivalent to g is already stored,
// the returned Op has no effect.
// It also sets field CreatedAt to the time g is stored (the current time),
// or to the time the original grant was stored, if there is one.
func (s *Store) Save(ctx context.Context, g *Grant) sinkdb.Op {
	key := s.keyPrefix + g.Policy
	if g.CreatedAt == "" {
		g.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	var grantList GrantList
	_, err := s.sdb.Get(ctx, key, &grantList)
	if err != nil {
		return sinkdb.Error(errors.Wrap(err))
	}

	grants := grantList.Grants
	for _, existing := range grants {
		if EqualGrants(*existing, *g) {
			// this grant already exists, do nothing
			g.CreatedAt = existing.CreatedAt
			return sinkdb.Op{}
		}
	}

	// create new grant and it append to the list of grants associated with this policy
	grants = append(grants, g)

	// TODO(tessr): Make this safe for concurrent updates. Will likely require a
	// conditional write operation for sinkdb
	return sinkdb.Set(s.keyPrefix+g.Policy, &GrantList{Grants: grants})
}

// Delete returns an Op to delete from policy all stored grants for which delete returns true.
func (s *Store) Delete(ctx context.Context, policy string, delete func(*Grant) bool) sinkdb.Op {
	key := s.keyPrefix + policy

	var grantList GrantList
	found, err := s.sdb.Get(ctx, key, &grantList)
	if err != nil || !found {
		return sinkdb.Error(errors.Wrap(err)) // if !found, errors.Wrap(err) is nil
	}

	var keep []*Grant
	for _, g := range grantList.Grants {
		if !delete(g) {
			keep = append(keep, g)
		}
	}

	// We didn't match any grants, don't need to do an update. Return no-op.
	if len(keep) == len(grantList.Grants) {
		return sinkdb.Op{}
	}

	// TODO(tessr): Make this safe for concurrent updates. Will likely require a
	// conditional write operation for sinkdb
	return sinkdb.Set(key, &GrantList{Grants: keep})
}

func EqualGrants(a, b Grant) bool {
	return a.GuardType == b.GuardType && bytes.Equal(a.GuardData, b.GuardData) && a.Protected == b.Protected
}