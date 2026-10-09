package store

import (
	"context"
	"fmt"
)

type User struct {
	Id   int
	Name string
}

type UserStore struct {
	mu    sync.Mutex
	cache map[int]*User
}

type Alias = User

var ErrNotFound = errors.New("not found")

const MaxUsers = 100

func NewUserStore() *UserStore {
	return &UserStore{}
}

func (s *UserStore) GetUser(ctx context.Context, id int) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.cache[id]; ok {
		return u, nil
	}
	return nil, ErrNotFound
}

func (s UserStore) Len() int {
	return len(s.cache)
}
