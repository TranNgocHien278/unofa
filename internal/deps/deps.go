// Package deps pins the approved dependency set (docs/CONTRACTS.md §Not in scope)
// so `go mod tidy` keeps them available to packages that are built later.
package deps

import (
	_ "github.com/gorilla/websocket"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/redis/go-redis/v9"
	_ "golang.org/x/crypto/bcrypt"
	_ "gopkg.in/yaml.v3"
)
