package database

import (
	"github.com/bradfitz/gomemcache/memcache"
)

func NewMemcached(server string) *memcache.Client {
	return memcache.New(server)
}
