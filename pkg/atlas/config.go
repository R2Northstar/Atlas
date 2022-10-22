// Package atlas runs the Atlas server.
package atlas

import (
	"fmt"
	"io/fs"
	"os/user"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type UIDGID [2]int

// Config contains the configuration for Atlas. The env struct tag contains the
// environment variable name and the default value if missing, or empty (if not
// ?=). All string arrays are comma-separated.
type Config struct {
	// The addresses to listen on (comma-separated).
	Addr []string `env:"ATLAS_ADDR?=:8081"`

	// The addresses to listen on with TLS (comma-separated).
	AddrTLS []string `env:"ATLAS_ADDR_HTTPS"`

	// Whether to trust Cloudflare headers like CF-Connecting-IP.
	//
	// This is not safe to use unless you:
	//  - Set Host to prevent it from being accessed via other CF zones.
	//  - Use an IP whitelist, or client certificates with mTLS-only origin pull.
	Cloudflare bool `env:"ATLAS_CLOUDFLARE"`

	// Comma-separated list of case-insensitive hostnames to accept via the Host
	// header. If not provided, all hostnames are allowed.
	Host []string `env:"ATLAS_HOST"`

	// Comma-separated list of paths to SSL server certificates to use for SSL.
	// The .crt and .key extensions will be appended automatically. If not
	// provided, SSL is disabled.
	ServerCerts []string `env:"ATLAS_SERVER_CERTS"`

	// Comma-separated list of paths to SSL CA certificates to use for SSL
	// client authentication. No effect is ServerCerts is not provided. If not
	// provided, clients are not required to use SSL client authentication.
	// TODO: ClientCerts []string `env:"ATLAS_CLIENT_CERTS"`

	// The minimum log level (e.g., trace, debug, info, warn, error, fatal).
	//
	// Note that access logs for noisy HTTP endpoints are demoted to debug.
	LogLevel zerolog.Level `env:"ATLAS_LOG_LEVEL=debug"`

	// Whether to log to stdout.
	LogStdout bool `env:"ATLAS_LOG_STDOUT=true"`

	// Whether to use pretty logs.
	LogStdoutPretty bool `env:"ATLAS_LOG_STDOUT_PRETTY=true"`

	// The minimum log level for stdout.
	LogStdoutLevel zerolog.Level `env:"ATLAS_LOG_STDOUT_LEVEL=trace"`

	// The log file to output to, if provided. Reopened on SIGHUP.
	LogFile string `env:"ATLAS_LOG_FILE"`

	// The minimum log level for the log file.
	LogFileLevel zerolog.Level `env:"ATLAS_LOG_FILE_LEVEL=info"`

	// The permissions for the log file.
	LogFileChmod fs.FileMode `env:"ATLAS_LOG_FILE_CHMOD"`

	// The owner for the log file. Not supported on Windows.
	LogFileChown *UIDGID `env:"ATLAS_LOG_FILE_CHOWN"`

	// Maps source IP prefixes to another IP (useful for controlling server
	// registration IPs when running within a LAN and port forwarding during
	// development). Comma-separated list of prefix=ip (example:
	// 192.168.0.0/24=1.2.3.4).
	DevMapIP []string `env:"ATLAS_DEV_MAP_IP"`

	// The maximum number of gameservers to allow. If -1, no limit is applied.
	API0_MaxServers int `env:"ATLAS_API0_MAX_SERVERS=1000"`

	// The maximum number of gameservers to allow per IP. If -1, no limit is
	// applied.
	API0_MaxServersPerIP int `env:"ATLAS_API0_MAX_SERVERS_PER_IP=25"`

	// The amount of time for player masterserver auth tokens to be valid for.
	API0_TokenExpiryTime time.Duration `env:"ATLAS_API0_TOKEN_EXPIRY_TIME=24h"`

	// Don't check player masterserver auth tokens, disable stryder auth.
	API0_InsecureDevNoCheckPlayerAuth bool `env:"ATLAS_API0_INSECURE_DEV_NO_CHECK_PLAYER_AUTH"`

	// Whether to allow games to register via IPv6. Not recommended.
	API0_AllowGameServerIPv6 bool `env:"ATLAS_API0_ALLOW_GAME_SERVER_IPV6"`

	// Minimum launcher semver to allow for servers or authenticated clients.
	// Dev versions are always allowed. If not provided, all client versions are
	// allowed.
	API0_MinimumLauncherVersion string `env:"ATLAS_API0_MINIMUM_LAUNCHER_VERSION"`

	// The time after registration for a gameserver to complete verification by.
	API0_ServerList_VerifyTime time.Duration `env:"ATLAS_API0_SERVERLIST_VERIFY_TIME=10s"`

	// The time since the last heartbeat for a gameserver to be marked as
	// dead.
	API0_ServerList_DeadTime time.Duration `env:"ATLAS_API0_SERVERLIST_DEAD_TIME=30s"`

	// The time since the last heartbeat for a gameserver to be discarded (i.e.,
	// it can't be added again without re-verifying).
	API0_ServerList_GhostTime time.Duration `env:"ATLAS_API0_SERVERLIST_GHOST_TIME=2m"`

	// Experimental option to use deterministic server ID generation based on
	// the provided secret and the server info. The secret is used to prevent
	// brute-forcing server IDs from the ID and known server info.
	API0_ServerList_ExperimentalDeterministicServerIDSecret string `env:"ATLAS_API0_SERVERLIST_EXPERIMENTAL_DETERMINISTIC_SERVER_ID_SECRET"`

	// The storage to use for accounts:
	//  - memory
	//  - sqlite3:/path/to/atlas.db
	API0_Storage_Accounts string `env:"ATLAS_API0_STORAGE_ACCOUNTS=memory"`

	// The storage to use for pdata:
	//  - memory:compress
	//  - sqlite3:/path/to/pdata.db
	API0_Storage_Pdata string `env:"ATLAS_API0_STORAGE_PDATA=memory:compress"`

	// The source to use for mainmenupromos:
	//  - none
	//  - file:/path/to/mainmenupromos.json
	API0_MainMenuPromos string `env:"ATLAS_API0_MAINMENUPROMOS=none"`

	// The email address to use for Origin login. If not provided, usernames are not
	// resolved during authentication.
	OriginEmail string `env:"ATLAS_ORIGIN_EMAIL"`

	// The password for Origin login.
	OriginPassword string `env:"ATLAS_ORIGIN_PASSWORD"`

	// The JSON file to save Origin login info to so tokens are preserved across
	// restarts. Highly recommended.
	OriginPersist string `env:"ATLAS_ORIGIN_PERSIST"`

	// Secret token for accessing internal metrics.
	MetricsSecret string `env:"ATLAS_METRICS_SECRET"`

	// The path to use for static website files. If a file named redirects.json
	// exists, it is read at startup, reloaded on SIGHUP, and used as a mapping
	// of top-level names to URLs.
	Web string `env:"ATLAS_WEB="`

	// For sd-notify.
	NotifySocket string `env:"NOTIFY_SOCKET"`

	// TODO: BadWords
}

// UnmarshalEnv unmarshals an array of environment variables into c, setting
// default values as appropriate. If incremental is true, default values will
// not be set for missing env vars, but only for empty ones.
func (c *Config) UnmarshalEnv(es []string, incremental bool) error {
	em := map[string]string{}
	for _, e := range es {
		if strings.HasPrefix(e, "ATLAS_") || strings.HasPrefix(e, "NOTIFY_SOCKET=") {
			if k, v, ok := strings.Cut(e, "="); ok {
				em[k] = v
			}
		}
	}
	cv := reflect.ValueOf(c).Elem()
	for _, ctf := range reflect.VisibleFields(cv.Type()) {
		env, ok := ctf.Tag.Lookup("env")
		if !ok {
			continue
		}
		var unsettable bool
		key, val, _ := strings.Cut(env, "=")
		if strings.HasSuffix(key, "?") {
			key = strings.TrimSuffix(key, "?")
			unsettable = true
		}
		if v, exists := em[key]; exists {
			if unsettable || v != "" {
				val = v
			}
			delete(em, key)
		} else if incremental {
			continue
		}
		switch cvf := cv.FieldByName(ctf.Name); cvf.Interface().(type) {
		case string:
			cvf.SetString(val)
		case int, int8, int16, int32, int64:
			if val == "" {
				cvf.SetInt(0)
			} else if v, err := strconv.ParseInt(val, 10, 64); err == nil {
				cvf.SetInt(v)
			} else {
				return fmt.Errorf("env %s (%T): parse %q: %w", key, cvf.Interface(), val, err)
			}
		case bool:
			if val == "" {
				cvf.SetBool(false)
			} else if v, err := strconv.ParseBool(val); err == nil {
				cvf.SetBool(v)
			} else {
				return fmt.Errorf("env %s (%T): parse %q: %w", key, cvf.Interface(), val, err)
			}
		case []string:
			if val == "" {
				cvf.Set(reflect.ValueOf([]string{}))
			} else {
				cvf.Set(reflect.ValueOf(strings.Split(val, ",")))
			}
		case zerolog.Level:
			if v, err := zerolog.ParseLevel(val); err == nil {
				cvf.Set(reflect.ValueOf(v))
			} else {
				return fmt.Errorf("env %s (%T): parse %q: %w", key, cvf.Interface(), val, err)
			}
		case time.Duration:
			if v, err := time.ParseDuration(val); err == nil {
				cvf.Set(reflect.ValueOf(v))
			} else {
				return fmt.Errorf("env %s (%T): parse %q: %w", key, cvf.Interface(), val, err)
			}
		case fs.FileMode:
			if val == "" {
				cvf.Set(reflect.ValueOf(fs.FileMode(0)))
			} else if v, err := strconv.ParseUint(val, 8, 32); err == nil {
				cvf.Set(reflect.ValueOf(fs.FileMode(v)))
			} else {
				return fmt.Errorf("env %s (%T): parse %q: %w", key, cvf.Interface(), val, err)
			}
		case *UIDGID:
			if val == "" {
				cvf.Set(reflect.ValueOf((*UIDGID)(nil)))
			} else if v, err := parseUIDGID(val); err == nil {
				cvf.Set(reflect.ValueOf(&v))
			} else {
				return fmt.Errorf("env %s (%T): parse %q: %w", key, cvf.Interface(), val, err)
			}
		default:
			return fmt.Errorf("unhandled type %T (%s)", cvf.Interface(), env)
		}
	}
	for key, val := range em {
		if val != "" {
			return fmt.Errorf("unknown environment variable %q", key)
		}
	}
	return nil
}

func parseUIDGID(s string) (UIDGID, error) {
	var u UIDGID

	if runtime.GOOS == "windows" {
		return u, fmt.Errorf("not supported on windows")
	}
	if s == "" {
		return u, fmt.Errorf("must not be empty")
	}

	su, sg, hg := strings.Cut(s, ":")

	if su == "" || sg == "" {
		if x, err := user.Current(); err != nil {
			return u, fmt.Errorf("get current user: %w", err)
		} else if uid, err := strconv.ParseInt(x.Uid, 10, 64); err != nil {
			return u, fmt.Errorf("get current user: parse uid %q: %w", x.Uid, err)
		} else if gid, err := strconv.ParseInt(x.Gid, 10, 64); err != nil {
			return u, fmt.Errorf("get current user: parse gid %q: %w", x.Gid, err)
		} else {
			u = UIDGID{int(uid), int(gid)}
		}
	}
	if su != "" {
		if uid, err := strconv.ParseInt(su, 10, 64); err == nil {
			u[0] = int(uid)
		} else if x, err := user.Lookup(su); err != nil {
			return u, fmt.Errorf("get user: %w", err)
		} else if uid, err := strconv.ParseInt(x.Uid, 10, 64); err != nil {
			return u, fmt.Errorf("get user: parse uid %q: %w", x.Uid, err)
		} else {
			if !hg && sg == "" && x.Gid != "" {
				if gid, err := strconv.ParseInt(x.Gid, 10, 64); err != nil {
					return u, fmt.Errorf("get user: parse gid %q: %w", x.Gid, err)
				} else {
					u[1] = int(gid)
				}
			}
			u[0] = int(uid)
		}
	}
	if sg != "" {
		if gid, err := strconv.ParseInt(sg, 10, 64); err == nil {
			u[1] = int(gid)
		} else if x, err := user.LookupGroup(sg); err != nil {
			return u, fmt.Errorf("lookup group: %w", err)
		} else if gid, err := strconv.ParseInt(x.Gid, 10, 64); err != nil {
			return u, fmt.Errorf("lookup group: parse gid %q: %w", x.Gid, err)
		} else {
			u[1] = int(gid)
		}
	}
	return u, nil
}
