// Package atlas runs the Atlas server.
package atlas

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
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
	Addr []string `env:"ATLAS_ADDR?=:8080"`

	// The addresses to listen on with TLS (comma-separated).
	AddrTLS []string `env:"ATLAS_ADDR_HTTPS"`

	// The address to listen on and use to send connectionless packets. If the
	// port is 0, a random one is chosen.
	AddrUDP netip.AddrPort `env:"ATLAS_ADDR_UDP=:0"`

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
	// provided, SSL is disabled. If a path begins with @, it is treated as a
	// systemd credential name (i.e., @mycert expands to
	// $CREDENTIALS_DIRECTORY/mycert.{crt,key}).
	ServerCerts []string `env:"ATLAS_SERVER_CERTS" sdcreds:"expand,list"`

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

	// The path to a directory containing lexically-sorted rulesets.
	Rules string `env:"ATLAS_RULES"`

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

	// Minimum launcher semver to allow for authenticated clients, replacing
	// ATLAS_API0_MINIMUM_LAUNCHER_VERSION. Dev versions are always allowed. If
	// not provided, API0_MinimumLauncherVersion is used.
	API0_MinimumLauncherVersionClient string `env:"ATLAS_API0_MINIMUM_LAUNCHER_VERSION_CLIENT"`

	// Minimum launcher semver to allow for servers, replacing
	// ATLAS_API0_MINIMUM_LAUNCHER_VERSION. Dev versions are always allowed. If
	// not provided, API0_MinimumLauncherVersion is used.
	API0_MinimumLauncherVersionServer string `env:"ATLAS_API0_MINIMUM_LAUNCHER_VERSION_SERVER"`

	// Region mapping to use for server list. If set to an empty string or
	// "none", region maps are disabled. Options: none, default.
	API0_RegionMap string `env:"ATLAS_API0_REGION_MAP?=default"`

	// Region mapping overrides. Comma-separated list of prefix=region.
	API0_RegionMap_Override []string `env:"ATLAS_API0_REGION_MAP_OVERRIDE"`

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
	// brute-forcing server IDs from the ID and known server info. If it begins
	// with @, it is treated as the name of a systemd credential to load.
	API0_ServerList_ExperimentalDeterministicServerIDSecret string `env:"ATLAS_API0_SERVERLIST_EXPERIMENTAL_DETERMINISTIC_SERVER_ID_SECRET" sdcreds:"load,trimspace"`

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

	// If provided, the mainmenupromos will be merged with the provided source
	// (same syntax as API0_MAINEMENUPROMOS) if the client is older than the
	// API0_MinimumLauncherVersion.
	API0_MainMenuPromos_UpdateNeeded string `env:"ATLAS_API0_MAINMENUPROMOS_UPDATENEEDED=none"`

	// Sets the source used for resolving usernames. If not specified, "origin"
	// is used if OriginEmail is provided, otherwise, "none" is used.
	//  - none (don't get usernames)
	//  - origin (get the username from the Origin API)
	//  - origin-eax (get the username from the Origin API, but fall back to EAX on failure)
	//  - origin-eax-debug (get the username from the Origin API, but also check EAX and warn if it's different)
	//  - eax (get the username from EAX)
	//  - eax-origin (get the username from EAX, but fall back to the Origin API on failure)
	UsernameSource string `env:"ATLAS_USERNAMESOURCE"`

	// The email address to use for Origin login. If not provided, the Origin
	// API will not be used. If it begins with @, it is treated as the name of a
	// systemd credential to load.
	OriginEmail string `env:"ATLAS_ORIGIN_EMAIL" sdcreds:"load,trimspace"`

	// The password for Origin login. If it begins with @, it is treated as the
	// name of a systemd credential to load.
	OriginPassword string `env:"ATLAS_ORIGIN_PASSWORD" sdcreds:"load,trimspace"`

	// The base32 TOTP secret for Origin login. If it begins with @, it is
	// treated as the name of a systemd credential to load.
	OriginTOTP string `env:"ATLAS_ORIGIN_TOTP" sdcreds:"load,trimspace"`

	// OriginHARGzip controls whether to compress saved HAR archives.
	OriginHARGzip bool `env:"ATLAS_ORIGIN_HAR_GZIP"`

	// OriginHARSuccess is the path to a directory to save HAR archives of
	// successful Origin auth attempts.
	OriginHARSuccess string `env:"ATLAS_ORIGIN_HAR_SUCCESS"`

	// OriginHARError is the path to a directory to save HAR archives of
	// successful Origin auth attempts.
	OriginHARError string `env:"ATLAS_ORIGIN_HAR_ERROR"`

	// The JSON file to save Origin login info to so tokens are preserved across
	// restarts. Highly recommended.
	OriginPersist string `env:"ATLAS_ORIGIN_PERSIST"`

	// Override the EAX EA App version. If specified, updates will not be
	// checked automatically.
	EAXUpdateVersion string `env:"EAX_UPDATE_VERSION"`

	// EAXUpdateInterval is the min interval at which to check for EA App
	// updates.
	EAXUpdateInterval time.Duration `env:"EAX_UPDATE_INTERVAL=24h"`

	// EAXUpdateBucket is the update bucket to use when checking for EA App
	// updates.
	EAXUpdateBucket int `env:"EAX_UPDATE_BUCKET=0"`

	// Secret token for accessing internal metrics. If it begins with @, it is
	// treated as the name of a systemd credential to load.
	MetricsSecret string `env:"ATLAS_METRICS_SECRET" sdcreds:"load,trimspace"`

	// The path to use for static website files. If a file named redirects.json
	// exists, it is read at startup, reloaded on SIGHUP, and used as a mapping
	// of top-level names to URLs. Custom error pages can be named
	// {status}.html.
	Web string `env:"ATLAS_WEB"`

	// For the Funny:tm:
	AllowJokes bool `env:"ATLAS_JOKES"`

	// The path to the IP2Location database, which should contain at least the
	// country and region fields. The database must not be modified while atlas
	// is running, but it can be replaced (and a reload can be triggered with
	// SIGHUP). If not provided, geolocation-dependent features like server
	// regions and geo metrics will not be enabled. If it doesn't include latlon
	// info, geo metrics will be disabled too.
	IP2Location string `env:"ATLAS_IP2LOCATION"`

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

		// get the default value, and check if it can be explicitly set to an
		// empty value
		var unsettable bool
		key, val, _ := strings.Cut(env, "=")
		if strings.HasSuffix(key, "?") {
			key = strings.TrimSuffix(key, "?")
			unsettable = true
		}
		if v, exists := em[key]; exists {
			// expand credentials before attempting to set the var or checking
			// if it can be set to an empty value
			v, err := sdcreds(v, ctf.Tag.Get("sdcreds"))
			if err != nil {
				return fmt.Errorf("env %s: expand systemd credentials: %w", key, err)
			}

			// if the value is non-empty or we are allowed to set it to an empty
			// value, set it, otherwise simply keep the default
			if unsettable || v != "" {
				val = v
			}

			// we're finished processing this var
			delete(em, key)
		} else if incremental {
			// if we're only doing incremental updates, don't use the default
			// value if the current env list doesn't have the var
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
		case netip.AddrPort:
			if val == "" {
				cvf.Set(reflect.ValueOf(netip.AddrPort{}))
			} else if v, err := netip.ParseAddrPort(val); err == nil {
				cvf.Set(reflect.ValueOf(v))
			} else if v, err1 := netip.ParseAddrPort("[::]" + val); val[0] == ':' && err1 == nil {
				cvf.Set(reflect.ValueOf(v))
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

// sdcreds expands systemd credentials in v (prefixed by "@") according to tag,
// which consists of a mode followed by optional flags.
//
// Mode:
//   - (none): return the original value
//   - expand: expand to the cred path
//   - load: read the cred contents
//
// Args:
//   - trimspace (load): trim leading/trailing whitespace from the cred value
//   - list (expand, load): split v by "," and process each item individually
func sdcreds(v string, tag string) (string, error) {
	if tag == "" {
		return v, nil
	}

	var mode struct {
		expand bool
		load   bool
	}
	var opts struct {
		trimspace bool
		list      bool
	}

	tag, args, _ := strings.Cut(tag, ",")
	switch tag {
	case "expand":
		mode.expand = true
	case "load":
		mode.load = true
	default:
		return "", fmt.Errorf("invalid struct tag %q", tag)
	}
	for _, arg := range strings.Split(args, ",") {
		switch {
		case mode.load && arg == "trimspace":
			opts.trimspace = true
		case (mode.load || mode.expand) && arg == "list":
			opts.list = true
		default:
			return "", fmt.Errorf("invalid struct tag %q arg %q", tag, arg)
		}
	}

	var vs []string
	if opts.list {
		vs = strings.Split(v, ",")
	} else {
		vs = []string{v}
	}

	vsi := make([]int, 0, len(vs))
	for i, x := range vs {
		if len(x) != 0 && x[0] == '@' {
			vsi = append(vsi, i)
		}
	}
	if len(vsi) == 0 {
		return v, nil
	}
	if mode.expand || mode.load {
		crd := os.Getenv("CREDENTIALS_DIRECTORY")
		if crd == "" {
			return "", fmt.Errorf("expand %q: systemd CREDENTIALS_DIRECTORY env var not set", v)
		}
		if !filepath.IsAbs(crd) {
			return "", fmt.Errorf("expand %q: systemd CREDENTIALS_DIRECTORY=%q env var is not an absolute path", v, crd)
		}
		for _, i := range vsi {
			cred := vs[i][1:]
			if strings.Contains(cred, "/") || strings.Contains(cred, string(filepath.Separator)) {
				return "", fmt.Errorf("expand %q: invalid credential name %q", v, cred)
			}
			vs[i] = filepath.Join(crd, cred)
		}
	}
	if mode.load {
		for _, i := range vsi {
			pt := vs[i]
			buf, err := os.ReadFile(pt)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return v, fmt.Errorf("expand %q: no such credential %q", v, filepath.Base(pt))
				}
				return v, fmt.Errorf("expand %q: read credential %q: %w", v, filepath.Base(pt), err)
			}
			if opts.trimspace {
				buf = bytes.TrimSpace(buf)
			}
			vs[i] = string(buf)
		}
	}
	return strings.Join(vs, ","), nil
}
