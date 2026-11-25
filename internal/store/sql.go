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
		sniffing TEXT NOT NULL DEFAULT '{"enabled":true,"destOverride":["http","tls","quic"],"metadataOnly":false,"routeOnly":false}', raw TEXT NOT NULL,
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
	`CREATE TABLE IF NOT EXISTS outbounds (
		id TEXT PRIMARY KEY, tag TEXT NOT NULL, remark TEXT NOT NULL, protocol TEXT NOT NULL,
		address TEXT NOT NULL DEFAULT '', port BIGINT NOT NULL DEFAULT 0,
		username TEXT NOT NULL DEFAULT '', password TEXT NOT NULL DEFAULT '',
		relay_tag TEXT NOT NULL DEFAULT '', stream_json TEXT NOT NULL DEFAULT 'null',
		settings TEXT NOT NULL DEFAULT 'null', enable BIGINT NOT NULL DEFAULT 1)`,
	`CREATE TABLE IF NOT EXISTS routing_rules (
		id TEXT PRIMARY KEY, remark TEXT NOT NULL DEFAULT '', outbound_tag TEXT NOT NULL,
		inbound_tags TEXT NOT NULL DEFAULT '[]', domains TEXT NOT NULL DEFAULT '[]',
		ips TEXT NOT NULL DEFAULT '[]', port TEXT NOT NULL DEFAULT '',
		network TEXT NOT NULL DEFAULT '', protocol TEXT NOT NULL DEFAULT '[]',
		priority BIGINT NOT NULL DEFAULT 0, enable BIGINT NOT NULL DEFAULT 1)`,
	`CREATE TABLE IF NOT EXISTS quota_groups (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, total_bytes BIGINT NOT NULL DEFAULT 0,
		enable BIGINT NOT NULL DEFAULT 1)`,
	`CREATE TABLE IF NOT EXISTS nodes (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, address TEXT NOT NULL,
		token TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '')`,
	`CREATE TABLE IF NOT EXISTS api_tokens (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, token TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT '', expires_at BIGINT NOT NULL DEFAULT 0)`,
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
	`ALTER TABLE clients ADD COLUMN quota_group TEXT NOT NULL DEFAULT ''`,
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
		id, inbound_id, email, config, uuid, password, flow, sub_id, tg_id, quota_group, comment,
		enable, total_bytes, expiry_at, first_seen_at, limit_ip, reset_days, last_reset_at,
		up, down, created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT (id) DO UPDATE SET
		inbound_id=excluded.inbound_id, email=excluded.email, config=excluded.config, uuid=excluded.uuid,
		password=excluded.password, flow=excluded.flow, sub_id=excluded.sub_id, tg_id=excluded.tg_id,
		comment=excluded.comment, enable=excluded.enable, total_bytes=excluded.total_bytes,
		expiry_at=excluded.expiry_at, first_seen_at=excluded.first_seen_at, limit_ip=excluded.limit_ip,
		reset_days=excluded.reset_days, last_reset_at=excluded.last_reset_at,
		up=excluded.up, down=excluded.down, created_at=excluded.created_at`

	upsertSetting = `INSERT INTO settings (k, v) VALUES (?,?) ON CONFLICT (k) DO UPDATE SET v=excluded.v`

	upsertOutbound = `INSERT INTO outbounds (
		id, tag, remark, protocol, address, port, username, password, relay_tag, stream_json, settings, enable
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT (id) DO UPDATE SET
		tag=excluded.tag, remark=excluded.remark, protocol=excluded.protocol, address=excluded.address,
		port=excluded.port, username=excluded.username, password=excluded.password, relay_tag=excluded.relay_tag,
		stream_json=excluded.stream_json, settings=excluded.settings, enable=excluded.enable`

	upsertRoutingRule = `INSERT INTO routing_rules (
		id, remark, outbound_tag, inbound_tags, domains, ips, port, network, protocol, priority, enable
	) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT (id) DO UPDATE SET
		remark=excluded.remark, outbound_tag=excluded.outbound_tag, inbound_tags=excluded.inbound_tags,
		domains=excluded.domains, ips=excluded.ips, port=excluded.port, network=excluded.network,
		protocol=excluded.protocol, priority=excluded.priority, enable=excluded.enable`

	upsertQuotaGroup = `INSERT INTO quota_groups (
		id, name, total_bytes, enable
	) VALUES (?,?,?,?) ON CONFLICT (id) DO UPDATE SET
		name=excluded.name, total_bytes=excluded.total_bytes, enable=excluded.enable`

	upsertNode = `INSERT INTO nodes (
		id, name, address, token, status
	) VALUES (?,?,?,?,?) ON CONFLICT (id) DO UPDATE SET
		name=excluded.name, address=excluded.address, token=excluded.token, status=excluded.status`

	upsertAPIToken = `INSERT INTO api_tokens (
		id, name, token, scope, expires_at
	) VALUES (?,?,?,?,?) ON CONFLICT (id) DO UPDATE SET
		name=excluded.name, token=excluded.token, scope=excluded.scope, expires_at=excluded.expires_at`

	upsertMeta = `INSERT INTO meta (k, v) VALUES (?,?) ON CONFLICT (k) DO UPDATE SET v=excluded.v`
	insertMeta = `INSERT INTO meta (k, v) VALUES (?,?) ON CONFLICT (k) DO NOTHING`
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
		_, _ = db.Exec(m)
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
	err = b.each(`SELECT id, inbound_id, email, config, uuid, password, flow, sub_id, tg_id, quota_group, comment, enable, total_bytes, expiry_at, first_seen_at, limit_ip, reset_days, last_reset_at, up, down, created_at FROM clients ORDER BY created_at, id`,
		func(r *sql.Rows) error {
			var c Client
			var en, limitIP, resetDays int64
			var cfg, ca string
			if err := r.Scan(
				&c.ID, &c.InboundID, &c.Email, &cfg, &c.UUID, &c.Password, &c.Flow, &c.SubID, &c.TgID, &c.QuotaGroup, &c.Comment,
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
	err = b.each(`SELECT id, tag, remark, protocol, address, port, username, password, relay_tag, stream_json, settings, enable FROM outbounds ORDER BY tag, id`,
		func(r *sql.Rows) error {
			var o Outbound
			var port, en int64
			var streamStr, setStr string
			if err := r.Scan(&o.ID, &o.Tag, &o.Remark, &o.Protocol, &o.Address, &port, &o.Username, &o.Password, &o.RelayTag, &streamStr, &setStr, &en); err != nil {
				return err
			}
			o.Port, o.Enable = int(port), en != 0
			o.Settings = decRaw(setStr)
			if streamStr != "" && streamStr != "null" {
				var strm OutboundStream
				if err := json.Unmarshal([]byte(streamStr), &strm); err == nil {
					o.Stream = &strm
				}
			}
			st.Outbounds = append(st.Outbounds, o)
			return nil
		})
	if err != nil {
		return st, err
	}
	err = b.each(`SELECT id, remark, outbound_tag, inbound_tags, domains, ips, port, network, protocol, priority, enable FROM routing_rules ORDER BY priority, id`,
		func(r *sql.Rows) error {
			var rr RoutingRule
			var prio, en int64
			var ibRaw, domRaw, ipRaw, protoRaw string
			if err := r.Scan(&rr.ID, &rr.Remark, &rr.OutboundTag, &ibRaw, &domRaw, &ipRaw, &rr.Port, &rr.Network, &protoRaw, &prio, &en); err != nil {
				return err
			}
			rr.Priority, rr.Enable = int(prio), en != 0
			_ = json.Unmarshal([]byte(ibRaw), &rr.InboundTags)
			_ = json.Unmarshal([]byte(domRaw), &rr.Domains)
			_ = json.Unmarshal([]byte(ipRaw), &rr.IPs)
			_ = json.Unmarshal([]byte(protoRaw), &rr.Protocol)
			st.RoutingRules = append(st.RoutingRules, rr)
			return nil
		})
	if err != nil {
		return st, err
	}
	err = b.each(`SELECT id, name, total_bytes, enable FROM quota_groups ORDER BY name, id`,
		func(r *sql.Rows) error {
			var q QuotaGroup
			var en int64
			if err := r.Scan(&q.ID, &q.Name, &q.TotalBytes, &en); err != nil {
				return err
			}
			q.Enable = en != 0
			st.QuotaGroups = append(st.QuotaGroups, q)
			return nil
		})
	if err != nil {
		return st, err
	}
	err = b.each(`SELECT id, name, address, token, status FROM nodes ORDER BY name, id`,
		func(r *sql.Rows) error {
			var n Node
			if err := r.Scan(&n.ID, &n.Name, &n.Address, &n.Token, &n.Status); err != nil {
				return err
			}
			st.Nodes = append(st.Nodes, n)
			return nil
		})
	if err != nil {
		return st, err
	}
	err = b.each(`SELECT id, name, token, scope, expires_at FROM api_tokens ORDER BY name, id`,
		func(r *sql.Rows) error {
			var t APIToken
			if err := r.Scan(&t.ID, &t.Name, &t.Token, &t.Scope, &t.ExpiresAt); err != nil {
				return err
			}
			st.APITokens = append(st.APITokens, t)
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
				c.SubID, c.TgID, c.QuotaGroup, c.Comment, flag(c.Enable), c.TotalBytes, c.ExpiryAt, c.FirstSeenAt,
				int64(c.LimitIP), int64(c.Reset), c.LastResetAt, c.Up, c.Down, encTime(c.CreatedAt))
		},
		func(id string) error { return exec(`DELETE FROM clients WHERE id = ?`, id) })
	if err != nil {
		return err
	}

	err = diff(prev.Outbounds, next.Outbounds, func(o Outbound) string { return o.ID },
		func(o Outbound) error {
			strmBytes, _ := json.Marshal(o.Stream)
			if o.Stream == nil {
				strmBytes = []byte("null")
			}
			return exec(upsertOutbound, o.ID, o.Tag, o.Remark, o.Protocol, o.Address, int64(o.Port), o.Username, o.Password, o.RelayTag, string(strmBytes), encRaw(o.Settings), flag(o.Enable))
		},
		func(id string) error { return exec(`DELETE FROM outbounds WHERE id = ?`, id) })
	if err != nil {
		return err
	}

	err = diff(prev.RoutingRules, next.RoutingRules, func(r RoutingRule) string { return r.ID },
		func(r RoutingRule) error {
			ibB, _ := json.Marshal(r.InboundTags)
			domB, _ := json.Marshal(r.Domains)
			ipB, _ := json.Marshal(r.IPs)
			proB, _ := json.Marshal(r.Protocol)
			return exec(upsertRoutingRule, r.ID, r.Remark, r.OutboundTag, string(ibB), string(domB), string(ipB), r.Port, r.Network, string(proB), int64(r.Priority), flag(r.Enable))
		},
		func(id string) error { return exec(`DELETE FROM routing_rules WHERE id = ?`, id) })
	if err != nil {
		return err
	}

	err = diff(prev.QuotaGroups, next.QuotaGroups, func(q QuotaGroup) string { return q.ID },
		func(q QuotaGroup) error { return exec(upsertQuotaGroup, q.ID, q.Name, q.TotalBytes, flag(q.Enable)) },
		func(id string) error { return exec(`DELETE FROM quota_groups WHERE id = ?`, id) })
	if err != nil {
		return err
	}

	err = diff(prev.Nodes, next.Nodes, func(n Node) string { return n.ID },
		func(n Node) error { return exec(upsertNode, n.ID, n.Name, n.Address, n.Token, n.Status) },
		func(id string) error { return exec(`DELETE FROM nodes WHERE id = ?`, id) })
	if err != nil {
		return err
	}

	err = diff(prev.APITokens, next.APITokens, func(t APIToken) string { return t.ID },
		func(t APIToken) error { return exec(upsertAPIToken, t.ID, t.Name, t.Token, t.Scope, t.ExpiresAt) },
		func(id string) error { return exec(`DELETE FROM api_tokens WHERE id = ?`, id) })
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
