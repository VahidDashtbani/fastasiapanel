package main

import (
	"fastasiapanel/internal/config"
	"fastasiapanel/internal/store"
)

func openStore(c config.Config) (*store.Store, error) {
	switch c.DBType {
	case "postgres":
		return store.OpenSQL("postgres", c.PostgresDSN)
	case "sqlite":
		return store.OpenSQL("sqlite", store.SQLiteDSN(c.DataFile))
	}
	return store.Open(c.DataFile)
}