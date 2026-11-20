package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

var schema = []string{
	`CREATE TABLE IF NOT EXISTS inbounds (
		id TEXT PRIMARY KEY, tag TEXT NOT NULL, remark TEXT NOT NULL, protocol TEXT NOT NULL,
		listen TEXT NOT NULL, port BIGINT NOT NULL, enable BIGINT NOT NULL,
		up BIGINT NOT NULL DEFAULT 0, down BIGINT NOT NULL DEFAULT 0,
		total_bytes BIGINT NOT NULL DEFAULT 0, expiry_at BIGINT NOT NULL DEFAULT 0,
		settings TEXT NOT NULL DEFAULT 'null', stream_settings TEXT NOT NULL DEFAULT 'null',
		sniffing TEXT NOT NULL DEFAULT 'null', raw TEXT NOT NULL,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS clients (
		id TEXT PRIMARY KEY, inbound_id TEXT NOT NULL, email TEXT NOT NULL, config TEXT NOT NULL,
		uuid TEXT NOT NULL, password TEXT NOT NULL DEFAULT '', flow TEXT NOT NULL DEFAULT '',
		sub_id TEXT NOT NULL DEFAULT '', tg_id TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '',
		enable BIGINT NOT NULL, total_bytes BIGINT NOT NULL, expiry_at BIGINT NOT NULL,
		first_seen_at BIGINT NOT NULL DEFAULT 0, limit_ip BIGINT NOT NULL DEFAULT 0,
		reset_days BIGINT NOT NULL DEFAULT 0, last_reset_at BIGINT NOT NULL DEFAULT 0,
		up BIGINT NOT NULL, down BIGINT NOT NULL, created_at TEXT NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS idx_clients_inbound ON clients (inbound_id)`,
	`CREATE INDEX IF NOT EXISTS idx_clients_sub_id ON clients (sub_id)`,
	`CREATE INDEX IF NOT EXISTS idx_clients_email ON clients (email)`,
	`CREATE TABLE IF NOT EXISTS settings (k TEXT PRIMARY KEY, v TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL)`,
}

var migrations = []string{
	`ALTER TABLE inbounds ADD COLUMN up BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE inbounds ADD COLUMN down BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE inbounds ADD COLUMN total_bytes BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE inbounds ADD COLUMN expiry_at BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE inbounds ADD COLUMN settings TEXT NOT NULL DEFAULT 'null'`,
	`ALTER TABLE inbounds ADD COLUMN stream_settings TEXT NOT NULL DEFAULT 'null'`,
	`ALTER TABLE inbounds ADD COLUMN sniffing TEXT NOT NULL DEFAULT 'null'`,
	`ALTER TABLE clients ADD COLUMN password TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE clients ADD COLUMN flow TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE clients ADD COLUMN sub_id TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE clients ADD COLUMN tg_id TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE clients ADD COLUMN comment TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE clients ADD COLUMN first_seen_at BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE clients ADD COLUMN limit_ip BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE clients ADD COLUMN reset_days BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE clients ADD COLUMN last_reset_at BIGINT NOT NULL DEFAULT 0`,
}

const (
	upsertInbound = `INSERT INTO inbounds (
		id, tag, remark, protocol, listen, port, enable, up, down, total_bytes, expiry_at,
		settings, stream_settings, sniffing, raw, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT (id) DO UPDATE SET
		tag=excluded.tag, remark=excluded.remark, protocol=excluded.protocol, listen=excluded.listen,
		port=excluded.port, enable=excluded.enable, up=excluded.up, down=excluded.down,
		total_bytes=excluded.total_bytes, expiry_at=excluded.expiry_at,
		settings=excluded.settings, stream_settings=excluded.stream_settings, sniffing=excluded.sniffing,
		raw=excluded.raw, created_at=excluded.created_at, updated_at=excluded.updated_at`

	upsertClient = `INSERT INTO clients (
		id, inbound_id, email, config, uuid, password, flow, sub_id, tg_id, comment,
		enable, total_bytes, expiry_at, first_seen_at, limit_ip, reset_days, last_reset_at,
		up, down, created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT (id) DO UPDATE SET
		inbound_id=excluded.inbound_id, email=excluded.email, config=excluded.config, uuid=excluded.uuid,
		password=excluded.password, flow=excluded.flow, sub_id=excluded.sub_id, tg_id=excluded.tg_id,
		comment=excluded.comment, enable=excluded.enable, total_bytes=excluded.total_bytes,
		expiry_at=excluded.expiry_at, first_seen_at=excluded.first_seen_at, limit_ip=excluded.limit_ip,
		reset_days=excluded.reset_days, last_reset_at=excluded.last_reset_at,
		up=excluded.up, down=excluded.down, created_at=excluded.created_at`

	upsertSetting = `INSERT INTO settings (k, v) VALUES (?,?) ON CONFLICT (k) DO UPDATE SET v=excluded.v`
	upsertMeta    = `INSERT INTO meta (k, v) VALUES (?,?) ON CONFLICT (k) DO UPDATE SET v=excluded.v`
	insertMeta    = `INSERT INTO meta (k, v) VALUES (?,?) ON CONFLICT (k) DO NOTHING`
)

type sqlBackend struct {
	db *sql.DB
	pg bool
}

func SQLiteDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(0)"
}

func OpenSQL(driver, dsn string) (*Store, error) {
	var name string
	switch driver {
	case "sqlite":
		name = "sqlite"
		if err := os.MkdirAll(filepath.Dir(strings.TrimPrefix(strings.SplitN(dsn, "?", 2)[0], "file:")), 0700); err != nil {
			return nil, err
		}
	case "postgres":
		name = "pgx"
	default:
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	db, err := sql.Open(name, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	b := &sqlBackend{db: db, pg: driver == "postgres"}
	for _, q := range schema {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	for _, m := range migrations {
		_, _ = db.Exec(m) // Ignore duplicate column errors on existing schemas
	}
	s, err := New(b)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (b *sqlBackend) q(s string) string {
	if !b.pg {
		return s
	}
	var out strings.Builder
	n := 0
	for _, r := range s {
		if r == '?' {
			n++
			out.WriteString("$" + strconv.Itoa(n))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func (b *sqlBackend) each(query string, fn func(*sql.Rows) error) error {
	rows, err := b.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err = fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func flag(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func encRaw(r json.RawMessage) string {
	if len(r) == 0 {
		return "null"
	}
	return string(r)
}

func decRaw(s string) json.RawMessage {
	if s == "" || s == "null" {
		return nil
	}
	return json.RawMessage(s)
}

func encTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func decTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (b *sqlBackend) Load() (State, error) {
	var st State
	err := b.each(`SELECT id, tag, remark, protocol, listen, port, enable, up, down, total_bytes, expiry_at, settings, stream_settings, sniffing, raw, created_at, updated_at FROM inbounds ORDER BY created_at, id`,
		func(r *sql.Rows) error {
			var in Inbound
			var port, en int64
			var setRaw, streamRaw, sniffRaw, raw, ca, ua string
			if err := r.Scan(
				&in.ID, &in.Tag, &in.Remark, &in.Protocol, &in.Listen, &port, &en,
				&in.Up, &in.Down, &in.TotalBytes, &in.ExpiryAt,
				&setRaw, &streamRaw, &sniffRaw, &raw, &ca, &ua,
			); err != nil {
				return err
			}
			in.Port, in.Enable = int(port), en != 0
			in.Settings, in.StreamSettings, in.Sniffing, in.Raw = decRaw(setRaw), decRaw(streamRaw), decRaw(sniffRaw), decRaw(raw)
			in.CreatedAt, in.UpdatedAt = decTime(ca), decTime(ua)
			st.Inbounds = append(st.Inbounds, in)
			return nil
		})
	if err != nil {
		return st, err
	}
	err = b.each(`SELECT id, inbound_id, email, config, uuid, password, flow, sub_id, tg_id, comment, enable, total_bytes, expiry_at, first_seen_at, limit_ip, reset_days, last_reset_at, up, down, created_at FROM clients ORDER BY created_at, id`,
		func(r *sql.Rows) error {
			var c Client
			var en, limitIP, resetDays int64
			var cfg, ca string
			if err := r.Scan(
				&c.ID, &c.InboundID, &c.Email, &cfg, &c.UUID, &c.Password, &c.Flow, &c.SubID, &c.TgID, &c.Comment,
				&en, &c.TotalBytes, &c.ExpiryAt, &c.FirstSeenAt, &limitIP, &resetDays, &c.LastResetAt,
				&c.Up, &c.Down, &ca,
			); err != nil {
				return err
			}
			c.Enable = en != 0
			c.LimitIP, c.Reset = int(limitIP), int(resetDays)
			c.Config, c.CreatedAt = decRaw(cfg), decTime(ca)
			st.Clients = append(st.Clients, c)
			return nil
		})
	if err != nil {
		return st, err
	}
	st.Settings = map[string]string{}
	err = b.each(`SELECT k, v FROM settings`, func(r *sql.Rows) error {
		var k, v string
		if err := r.Scan(&k, &v); err != nil {
			return err
		}
		st.Settings[k] = v
		return nil
	})
	if err != nil {
		return st, err
	}
	return st, b.each(`SELECT k, v FROM meta`, func(r *sql.Rows) error {
		var k, v string
		if err := r.Scan(&k, &v); err != nil {
			return err
		}
		switch k {
		case "created_at":
			st.CreatedAt = decTime(v)
		case "updated_at":
			st.UpdatedAt = decTime(v)
		}
		return nil
	})
}

func diff[T any](prev, next []T, id func(T) string, upsert func(T) error, remove func(string) error) error {
	old := make(map[string][]byte, len(prev))
	for _, p := range prev {
		raw, _ := json.Marshal(p)
		old[id(p)] = raw
	}
	for _, n := range next {
		raw, _ := json.Marshal(n)
		k := id(n)
		if o, ok := old[k]; ok {
			delete(old, k)
			if bytes.Equal(o, raw) {
				continue
			}
		}
		if err := upsert(n); err != nil {
			return err
		}
	}
	for k := range old {
		if err := remove(k); err != nil {
			return err
		}
	}
	return nil
}

func (b *sqlBackend) Save(prev, next State) error {
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) error {
		_, err := tx.Exec(b.q(q), args...)
		return err
	}

	err = diff(prev.Inbounds, next.Inbounds, func(i Inbound) string { return i.ID },
		func(i Inbound) error {
			return exec(upsertInbound, i.ID, i.Tag, i.Remark, i.Protocol, i.Listen, int64(i.Port), flag(i.Enable),
				i.Up, i.Down, i.TotalBytes, i.ExpiryAt,
				encRaw(i.Settings), encRaw(i.StreamSettings), encRaw(i.Sniffing), encRaw(i.Raw),
				encTime(i.CreatedAt), encTime(i.UpdatedAt))
		},
		func(id string) error { return exec(`DELETE FROM inbounds WHERE id = ?`, id) })
	if err != nil {
		return err
	}

	err = diff(prev.Clients, next.Clients, func(c Client) string { return c.ID },
		func(c Client) error {
			return exec(upsertClient, c.ID, c.InboundID, c.Email, encRaw(c.Config), c.UUID, c.Password, c.Flow,
				c.SubID, c.TgID, c.Comment, flag(c.Enable), c.TotalBytes, c.ExpiryAt, c.FirstSeenAt,
				int64(c.LimitIP), int64(c.Reset), c.LastResetAt, c.Up, c.Down, encTime(c.CreatedAt))
		},
		func(id string) error { return exec(`DELETE FROM clients WHERE id = ?`, id) })
	if err != nil {
		return err
	}

	for k, v := range next.Settings {
		if old, ok := prev.Settings[k]; ok && old == v {
			continue
		}
		if err = exec(upsertSetting, k, v); err != nil {
			return err
		}
	}
	for k := range prev.Settings {
		if _, ok := next.Settings[k]; !ok {
			if err = exec(`DELETE FROM settings WHERE k = ?`, k); err != nil {
				return err
			}
		}
	}

	if err = exec(insertMeta, "created_at", encTime(next.CreatedAt)); err != nil {
		return err
	}
	if err = exec(upsertMeta, "updated_at", encTime(next.UpdatedAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (b *sqlBackend) Close() error { return b.db.Close() }