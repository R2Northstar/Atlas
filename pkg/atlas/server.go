package atlas

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/klauspost/compress/gzip"
	"github.com/pg9182/ip2x"
	"github.com/r2northstar/atlas/db/atlasdb"
	"github.com/r2northstar/atlas/db/pdatadb"
	"github.com/r2northstar/atlas/pkg/api/api0"
	"github.com/r2northstar/atlas/pkg/cloudflare"
	"github.com/r2northstar/atlas/pkg/eax"
	"github.com/r2northstar/atlas/pkg/memstore"
	"github.com/r2northstar/atlas/pkg/nspkt"
	"github.com/r2northstar/atlas/pkg/origin"
	"github.com/r2northstar/atlas/pkg/regionmap"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"golang.org/x/mod/semver"
)

type Server struct {
	Logger zerolog.Logger

	Addr          []string
	AddrTLS       []string
	Handler       http.Handler
	Web           http.Handler
	Redirects     map[string]string
	NotifySocket  string
	MetricsSecret string
	API0          *api0.Handler
	Middleware    []func(http.Handler) http.Handler
	TLSConfig     *tls.Config

	reload []func()
	closed bool
}

// NewServer configures a new server using c, which is assumed to be initialized
// to default or configured values (as done by UnmarshalEnv). It will perform
// any additional config checks as required.
func NewServer(c *Config) (*Server, error) {
	if c.API0_MinimumLauncherVersion != "" && !semver.IsValid("v"+strings.TrimPrefix(c.API0_MinimumLauncherVersion, "v")) {
		return nil, fmt.Errorf("invalid minimum launcher version semver %q", c.API0_MinimumLauncherVersion)
	}
	if c.API0_MinimumLauncherVersionClient != "" && !semver.IsValid("v"+strings.TrimPrefix(c.API0_MinimumLauncherVersionClient, "v")) {
		return nil, fmt.Errorf("invalid minimum launcher client version semver %q", c.API0_MinimumLauncherVersionClient)
	}
	if c.API0_MinimumLauncherVersionServer != "" && !semver.IsValid("v"+strings.TrimPrefix(c.API0_MinimumLauncherVersionServer, "v")) {
		return nil, fmt.Errorf("invalid minimum launcher server version semver %q", c.API0_MinimumLauncherVersionServer)
	}

	var s Server
	var success bool

	s.Addr = c.Addr
	s.AddrTLS = c.AddrTLS

	s.NotifySocket = c.NotifySocket

	if c.Web != "" {
		if p, err := filepath.Abs(c.Web); err == nil {
			var redirects sync.Map
			var errpages sync.Map

			var err1 error
			reload := func() {
				var r map[string]string
				if buf, err := os.ReadFile(filepath.Join(p, "redirects.json")); err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						err1 = fmt.Errorf("read redirects.json: %w", err)
						return
					}
				} else if err = json.Unmarshal(buf, &r); err != nil {
					err1 = fmt.Errorf("read redirects.json: %w", err)
					return
				} else {
					redirects.Range(func(key, _ any) bool {
						redirects.Delete(key)
						return true
					})
					for p, u := range r {
						redirects.Store(strings.Trim(p, "/"), u)
					}
				}
				if es, err := os.ReadDir(filepath.Join(p)); err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						err1 = fmt.Errorf("read error pages: %w", err)
						return
					}
				} else {
					sc := map[int][]byte{}
					for _, e := range es {
						a, b, _ := strings.Cut(e.Name(), ".")
						if b != "html" {
							continue
						}
						s, err := strconv.ParseUint(a, 10, 64)
						if err != nil || s < 400 || s >= 600 {
							continue
						}
						c, err := os.ReadFile(filepath.Join(p, e.Name()))
						if err != nil {
							err1 = fmt.Errorf("read error page for %d: %w", s, err)
							return
						}
						sc[int(s)] = c
					}
					errpages.Range(func(key, _ any) bool {
						errpages.Delete(key)
						return true
					})
					for s, c := range sc {
						errpages.Store(s, c)
					}
				}

			}
			if reload(); err1 != nil {
				return nil, fmt.Errorf("initialize web: %w", err)
			}
			s.reload = append(s.reload, reload)

			fsrv := &statusInterceptor{
				Handler: http.FileServer(http.Dir(c.Web)),
				Error: func(s int) http.Handler {
					switch s {
					case http.StatusNotFound, http.StatusInternalServerError, http.StatusForbidden:
						if c, ok := errpages.Load(s); ok {
							b := c.([]byte)
							return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								w.Header().Set("Cache-Control", "private, no-cache, no-store, max-age=0, must-revalidate")
								w.Header().Set("Expires", "0")
								w.Header().Set("Pragma", "no-cache")
								w.Header().Set("Content-Type", "text/html; charset=utf-8")
								w.Header().Set("Content-Length", strconv.Itoa(len(b)))
								w.WriteHeader(s)
								w.Write(b)
							})
						}
					}
					return nil
				},
			}

			s.Web = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if v, ok := redirects.Load(strings.Trim(r.URL.Path, "/")); ok {
					http.Redirect(w, r, v.(string), http.StatusTemporaryRedirect)
					return
				}
				fsrv.ServeHTTP(w, r)
			})
		} else {
			return nil, fmt.Errorf("initialize web: resolve path: %w", err)
		}
	}

	if l, fn, err := configureLogging(c); err == nil {
		s.Logger = l
		s.reload = append(s.reload, fn)
	} else {
		return nil, fmt.Errorf("initialize logging: %w", err)
	}

	defer func() {
		if !success {
			if s.API0 != nil {
				if s.API0.AccountStorage != nil {
					if c, ok := s.API0.AccountStorage.(io.Closer); ok {
						c.Close()
					}
				}
				if s.API0.PdataStorage != nil {
					if c, ok := s.API0.PdataStorage.(io.Closer); ok {
						c.Close()
					}
				}
			}
		}
	}()

	var m middlewares

	if fn, err := configureDevMapIP(c); err != nil {
		return nil, fmt.Errorf("initialize DevMapIP: %w", err)
	} else if fn != nil {
		m.Add(fn)
	}

	m.Add(hlog.RequestIDHandler("", "X-Atlas-Request-Id"))

	if len(c.Host) != 0 {
		ns := map[string]struct{}{}
		for _, n := range c.Host {
			ns[strings.ToLower(n)] = struct{}{}
		}
		m.Add(func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				x := []byte(r.Host)
				for i := len(x) - 1; i >= 0; i-- {
					xc := x[i]
					if xc < '0' || xc > '9' {
						if xc == ':' {
							x = x[:i]
						}
						break
					}
				}
				if _, ok := ns[strings.ToLower(string(x))]; ok {
					h.ServeHTTP(w, r)
					return
				}
				w.Header().Set("Cache-Control", "private, no-cache, no-store")
				w.Header().Set("Expires", "0")
				w.Header().Set("Pragma", "no-cache")
				http.Error(w, "Go away.", http.StatusForbidden)
			})
		})
	}

	if c.Cloudflare {
		m.Add(cloudflare.RealIP(func(r *http.Request, err error) {
			e := s.Logger.Warn()
			if rid, ok := hlog.IDFromRequest(r); ok {
				e = e.Stringer("rid", rid)
			}
			e.
				Err(err).
				Str("component", "http").
				Str("request_ip", r.RemoteAddr).
				Msg("use cloudflare ip")
		}))
	}

	m.Add(hlog.AccessHandler(func(r *http.Request, status, size int, duration time.Duration) {
		e := s.Logger.Info()
		if rid, ok := hlog.IDFromRequest(r); ok {
			e = e.Stringer("rid", rid)
		}
		e.
			Str("request_ip", r.RemoteAddr).
			Str("request_host", r.Host).
			Str("request_method", r.Method).
			Stringer("request_uri", r.URL).
			Str("request_user_agent", r.UserAgent()).
			Int("response_status", status).
			Int("response_size", size).
			Dur("response_duration", duration).
			Msg("handle request")
	}))

	m.Add(hlog.NewHandler(s.Logger.With().Str("component", "api0").Logger()))
	m.Add(hlog.RequestIDHandler("rid", ""))

	s.API0 = &api0.Handler{
		NSPkt: nspkt.NewListener(),
		ServerList: api0.NewServerList(c.API0_ServerList_DeadTime, c.API0_ServerList_GhostTime, c.API0_ServerList_VerifyTime, api0.ServerListConfig{
			ExperimentalDeterministicServerIDSecret: c.API0_ServerList_ExperimentalDeterministicServerIDSecret,
			AllowUwuify:                             c.AllowJokes,
		}),
		MaxServers:                   c.API0_MaxServers,
		MaxServersPerIP:              c.API0_MaxServersPerIP,
		InsecureDevNoCheckPlayerAuth: c.API0_InsecureDevNoCheckPlayerAuth,
		MinimumLauncherVersionClient: c.API0_MinimumLauncherVersionClient,
		MinimumLauncherVersionServer: c.API0_MinimumLauncherVersionServer,
		TokenExpiryTime:              c.API0_TokenExpiryTime,
		AllowGameServerIPv6:          c.API0_AllowGameServerIPv6,
	}
	if v := c.API0_MinimumLauncherVersion; v != "" {
		if s.API0.MinimumLauncherVersionClient == "" {
			s.API0.MinimumLauncherVersionClient = v
		}
		if s.API0.MinimumLauncherVersionServer == "" {
			s.API0.MinimumLauncherVersionServer = v
		}
	}

	s.API0.NotFound = new(middlewares).
		Add(hlog.NewHandler(s.Logger)).
		Add(hlog.RequestIDHandler("rid", "")).
		Then(http.HandlerFunc(s.serveRest))

	if org, err := configureOrigin(c, s.Logger.With().Str("component", "origin").Logger()); err == nil {
		s.API0.OriginAuthMgr = org
	} else {
		return nil, fmt.Errorf("initialize origin auth: %w", err)
	}
	if exc, err := configureEAX(c, s.Logger.With().Str("component", "eax").Logger()); err == nil {
		s.API0.EAXClient = exc
	} else {
		return nil, fmt.Errorf("initialize eax: %w", err)
	}
	if x, err := configureUsernameSource(c); err == nil {
		s.API0.UsernameSource = x
	} else {
		return nil, fmt.Errorf("initialize username lookup: %w", err)
	}
	if astore, err := configureAccountStorage(c); err == nil {
		s.API0.AccountStorage = astore
	} else {
		return nil, fmt.Errorf("initialize account storage: %w", err)
	}
	if pstore, err := configurePdataStorage(c); err == nil {
		s.API0.PdataStorage = pstore
	} else {
		return nil, fmt.Errorf("initialize pdata storage: %w", err)
	}
	if mmp, err := configureMainMenuPromos(c); err == nil {
		s.API0.MainMenuPromos = mmp
	} else {
		return nil, fmt.Errorf("initialize main menu promos: %w", err)
	}
	if err := configureMainMenuPromosUpdateNeeded(c, s.API0); err != nil {
		return nil, fmt.Errorf("configure main menu promos when update needed: %w", err)
	}
	if ip2l, err := configureIP2Location(c); err == nil {
		if ip2l != nil {
			s.reload = append(s.reload, func() {
				if err := ip2l.Load(""); err != nil {
					s.Logger.Err(err).Msg("failed to reload ip2location database")
				}
			})
			s.API0.LookupIP = ip2l.LookupFields
		}
	} else {
		return nil, fmt.Errorf("initialize ip2location: %w", err)
	}
	if m, err := configureRegionMap(c); err == nil {
		s.API0.GetRegion = m
	} else {
		return nil, fmt.Errorf("initialize region map: %w", err)
	}

	s.MetricsSecret = c.MetricsSecret

	s.Handler = m.Then(s.API0)

	if cfg, err := configureServerTLS(c); err == nil {
		s.TLSConfig = cfg
	} else {
		return nil, fmt.Errorf("initialize server tls: %w", err)
	}

	if len(c.ServerCerts) != 0 {
		var certs []tls.Certificate
		for _, fn := range c.ServerCerts {
			cert, err := tls.LoadX509KeyPair(fn+".crt", fn+".key")
			if err != nil {
				return nil, fmt.Errorf("load server certificate %q: %w", fn, err)
			}
			certs = append(certs, cert)
		}
		s.TLSConfig = &tls.Config{
			Certificates: certs,
		}
	}

	success = true
	return &s, nil
}

func configureServerTLS(c *Config) (*tls.Config, error) {
	var t tls.Config
	if len(c.ServerCerts) != 0 {
		for _, fn := range c.ServerCerts {
			cert, err := tls.LoadX509KeyPair(fn+".crt", fn+".key")
			if err != nil {
				return nil, fmt.Errorf("load server certificate %q: %w", fn, err)
			}
			t.Certificates = append(t.Certificates, cert)
		}
	} else if len(c.AddrTLS) != 0 {
		return nil, fmt.Errorf("no tls certificates provided")
	}
	return &t, nil
}

func configureDevMapIP(c *Config) (func(http.Handler) http.Handler, error) {
	if len(c.DevMapIP) == 0 {
		return nil, nil
	}
	type devMapIPEntry struct {
		Prefix netip.Prefix
		Addr   netip.Addr
	}
	var ms []devMapIPEntry
	for _, m := range c.DevMapIP {
		a, b, ok := strings.Cut(m, "=")
		if !ok {
			return nil, fmt.Errorf("parse ip mapping %q: missing equals sign", m)
		}
		addr, err := netip.ParseAddr(b)
		if err != nil {
			return nil, fmt.Errorf("parse ip mapping %q: invalid address: %w", m, err)
		}
		if strings.ContainsRune(a, '/') {
			if pfx, err := netip.ParsePrefix(a); err == nil {
				ms = append(ms, devMapIPEntry{pfx, addr})
			} else {
				return nil, fmt.Errorf("parse ip mapping %q: invalid prefix: %w", m, err)
			}
		} else {
			if x, err := netip.ParseAddr(a); err == nil {
				if pfx, err := x.Prefix(x.BitLen()); err == nil {
					ms = append(ms, devMapIPEntry{pfx, addr})
				} else {
					panic(err)
				}
			} else {
				return nil, fmt.Errorf("parse ip mapping %q: invalid prefix: %w", m, err)
			}
		}
	}
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if x, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
				for _, m := range ms {
					if m.Prefix.Contains(x.Addr()) {
						r2 := *r
						r2.RemoteAddr = netip.AddrPortFrom(m.Addr, x.Port()).String()
						r = &r2
					}
				}
			}
			h.ServeHTTP(w, r)
		})
	}, nil
}

func configureLogging(c *Config) (l zerolog.Logger, reopen func(), err error) {
	var outputs []io.Writer
	if c.LogStdout {
		if c.LogStdoutPretty {
			outputs = append(outputs, newZerologWriterLevel(zerolog.ConsoleWriter{
				Out: os.Stdout,
			}, c.LogStdoutLevel))
		} else {
			outputs = append(outputs, newZerologWriterLevel(os.Stdout, c.LogStdoutLevel))
		}
	}
	if fn := c.LogFile; fn != "" {
		x := newZerologWriterLevel(nil, c.LogFileLevel)
		if fn, err = filepath.Abs(fn); err != nil {
			err = fmt.Errorf("resolve log file: %w", err)
			return
		}
		reopen = func() {
			x.SwapWriter(func(old io.Writer) io.Writer {
				if o, ok := old.(io.Closer); ok {
					o.Close()
				}
				if f, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666); err == nil {
					if c.LogFileChown != nil {
						if err := f.Chown((*c.LogFileChown)[0], (*c.LogFileChown)[1]); err != nil {
							fmt.Fprintf(os.Stderr, "error: chown log file: %v\n", err)
						}
					}
					if c.LogFileChmod != 0 {
						if err := f.Chmod(c.LogFileChmod); err != nil {
							fmt.Fprintf(os.Stderr, "error: chmod log file: %v\n", err)
						}
					}
					return f
				} else {
					fmt.Fprintf(os.Stderr, "error: failed to open log file: %v\n", err)
				}
				return nil
			})
		}
		outputs = append(outputs, x)
		reopen()
	}
	l = zerolog.New(zerolog.MultiLevelWriter(outputs...)).
		Level(c.LogLevel).
		With().
		Timestamp().
		Logger()
	return
}

func configureOrigin(c *Config, l zerolog.Logger) (*origin.AuthMgr, error) {
	if c.OriginEmail == "" {
		return nil, nil
	}
	var mu sync.Mutex
	mgr := &origin.AuthMgr{
		Credentials: func() (email, password, otpsecret string, err error) {
			return c.OriginEmail, c.OriginPassword, c.OriginTOTP, nil
		},
		Backoff: expbackoff,
		Updated: func(as origin.AuthState, err error) {
			mu.Lock()
			defer mu.Unlock()

			if fn := c.OriginPersist; fn != "" {
				if buf, err := json.Marshal(as); err != nil {
					l.Err(err).Msg("failed to save origin auth json")
					return
				} else if err = os.WriteFile(fn, buf, 0666); err != nil {
					l.Err(err).Msg("failed to save origin auth json")
					return
				}
			}
			if err != nil {
				l.Err(err).Msg("origin auth error; using old token")
			} else {
				l.Info().Msg("got new origin token")
			}
		},
	}
	if fn := c.OriginPersist; fn != "" {
		var as origin.AuthState
		if buf, err := os.ReadFile(fn); err != nil {
			if !os.IsNotExist(err) {
				l.Err(err).Msg("failed to load origin auth json")
			}
		} else if err := json.Unmarshal(buf, &as); err != nil {
			l.Err(err).Msg("failed to load origin auth json")
		} else {
			mgr.SetAuth(as)
		}
	}
	if c.OriginHARError != "" || c.OriginHARSuccess != "" {
		var errPath, successPath string
		if v := c.OriginHARError; v != "" {
			if p, err := filepath.Abs(v); err != nil {
				return nil, fmt.Errorf("resolve error har path: %w", err)
			} else if err := os.MkdirAll(v, 0777); err != nil {
				return nil, fmt.Errorf("mkdir error har path: %w", err)
			} else {
				errPath = p
			}
		}
		if v := c.OriginHARSuccess; v != "" {
			if p, err := filepath.Abs(v); err != nil {
				return nil, fmt.Errorf("resolve success har path: %w", err)
			} else if err := os.MkdirAll(v, 0777); err != nil {
				return nil, fmt.Errorf("mkdir success har path: %w", err)
			} else {
				successPath = p
			}
		}
		var harMu sync.Mutex
		harZ := gzip.NewWriter(io.Discard)
		mgr.SaveHAR = func(write func(w io.Writer) error, err error) {
			harMu.Lock()
			defer harMu.Unlock()

			var p string
			if err != nil {
				if errPath != "" {
					p = filepath.Join(errPath, "origin-auth-error-")
				}
			} else {
				if successPath != "" {
					p = filepath.Join(successPath, "origin-auth-success-")
				}
			}
			if p != "" {
				p = p + strconv.FormatInt(time.Now().Unix(), 10) + ".har"

				if c.OriginHARGzip {
					p += ".gz"
				}

				f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0600)
				if err != nil {
					l.Err(err).Msg("failed to save origin auth har")
					return
				}
				defer f.Close()

				if c.OriginHARGzip {
					harZ.Reset(f)
					if err := write(harZ); err != nil {
						l.Err(err).Msg("failed to save origin auth har")
						return
					}
					if err := harZ.Close(); err != nil {
						l.Err(err).Msg("failed to save origin auth har")
						return
					}
				} else {
					if err := write(f); err != nil {
						l.Err(err).Msg("failed to save origin auth har")
						return
					}
				}

				if err := f.Close(); err != nil {
					l.Err(err).Msg("failed to save origin auth har")
					return
				}
			}
		}
	}
	return mgr, nil
}

func configureEAX(c *Config, l zerolog.Logger) (*eax.Client, error) {
	mgr := &eax.UpdateMgr{
		AutoUpdateBackoff: expbackoff,
		AutoUpdateHook: func(ver string, err error) {
			if err != nil {
				l.Err(err).Str("eax_client_version", ver).Msg("eax update error, using old version")
			} else {
				l.Info().Str("eax_client_version", ver).Msg("updated eax client version")
			}
		},
	}
	if v := c.EAXUpdateVersion; v != "" {
		mgr.SetVersion(v)
	} else {
		mgr.AutoUpdateInterval = c.EAXUpdateInterval
		mgr.AutoUpdateBucket = c.EAXUpdateBucket
	}
	return &eax.Client{
		UpdateMgr: mgr,
	}, nil
}

func expbackoff(_ error, last time.Time, count int) bool {
	var hmax, hmaxat, hrate float64 = 24, 8, 2.3
	// ~5m, ~10m, ~23m, ~52m, ~2h, ~4.6h, ~10.5h, 24h

	var next float64
	if count >= int(hmaxat) {
		next = hmax
	} else {
		next = math.Pow(hrate, float64(count)) * hmax / math.Pow(hrate, hmaxat)
	}
	return time.Since(last).Hours() >= next
}

func configureUsernameSource(c *Config) (api0.UsernameSource, error) {
	switch typ := c.UsernameSource; typ {
	case "none":
		return api0.UsernameSourceNone, nil
	case "origin":
		return api0.UsernameSourceOrigin, nil
	case "origin-eax":
		return api0.UsernameSourceOriginEAX, nil
	case "origin-eax-debug":
		return api0.UsernameSourceOriginEAXDebug, nil
	case "eax":
		return api0.UsernameSourceEAX, nil
	case "eax-origin":
		return api0.UsernameSourceEAXOrigin, nil
	case "":
		// backwards compat
		if c.OriginEmail != "" {
			return api0.UsernameSourceOrigin, nil
		}
		return api0.UsernameSourceNone, nil
	default:
		return "", fmt.Errorf("unknown source %q", typ)
	}
}

func configureAccountStorage(c *Config) (api0.AccountStorage, error) {
	switch typ, arg, _ := strings.Cut(c.API0_Storage_Accounts, ":"); typ {
	case "memory":
		if arg != "" {
			return nil, fmt.Errorf("memory: invalid argument %q", arg)
		}
		return memstore.NewAccountStore(), nil
	case "sqlite3":
		p, err := filepath.Abs(arg)
		if err != nil {
			return nil, fmt.Errorf("sqlite3: resolve %q: %w", arg, err)
		}
		s, err := atlasdb.Open(p)
		if err != nil {
			return nil, fmt.Errorf("sqlite3: %w", err)
		}
		if cur, to, err := s.Version(); err != nil {
			return nil, fmt.Errorf("sqlite3: migrate: %w", err)
		} else if cur > to {
			return nil, fmt.Errorf("sqlite3: migrate: database version %d is too new", cur)
		} else if cur != to {
			if err := s.MigrateUp(context.Background(), to); err != nil {
				return nil, fmt.Errorf("sqlite3: migrate (%d to %d): %w", cur, to, err)
			}
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unknown type %q", typ)
	}
}

func configurePdataStorage(c *Config) (api0.PdataStorage, error) {
	switch typ, arg, _ := strings.Cut(c.API0_Storage_Pdata, ":"); typ {
	case "memory":
		switch arg {
		case "":
			return memstore.NewPdataStore(false), nil
		case "compress":
			return memstore.NewPdataStore(true), nil
		default:
			return nil, fmt.Errorf("memory: invalid argument %q", arg)
		}
	case "sqlite3":
		p, err := filepath.Abs(arg)
		if err != nil {
			return nil, fmt.Errorf("sqlite3: resolve %q: %w", arg, err)
		}
		s, err := pdatadb.Open(p)
		if err != nil {
			return nil, fmt.Errorf("sqlite3: %w", err)
		}
		if cur, to, err := s.Version(); err != nil {
			return nil, fmt.Errorf("sqlite3: migrate: %w", err)
		} else if cur > to {
			return nil, fmt.Errorf("sqlite3: migrate: database version %d is too new", cur)
		} else if cur != to {
			if err := s.MigrateUp(context.Background(), to); err != nil {
				return nil, fmt.Errorf("sqlite3: migrate (%d to %d): %w", cur, to, err)
			}
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unknown type %q", typ)
	}
}

func configureMainMenuPromos(c *Config) (func(*http.Request) api0.MainMenuPromos, error) {
	switch typ, arg, _ := strings.Cut(c.API0_MainMenuPromos, ":"); typ {
	case "none":
		return nil, nil
	case "file":
		p, err := filepath.Abs(arg)
		if err != nil {
			return nil, fmt.Errorf("file: resolve %q: %w", arg, err)
		}
		fn := func(*http.Request) api0.MainMenuPromos {
			var mmp api0.MainMenuPromos
			if buf, err1 := os.ReadFile(p); err1 != nil {
				err = err1
			} else if err = json.Unmarshal(buf, &mmp); err != nil {
				err = err1
			}
			return mmp
		}
		if fn(nil); err != nil {
			return nil, fmt.Errorf("file: %w", err)
		}
		return fn, nil
	default:
		return nil, fmt.Errorf("unknown source %q", typ)
	}
}

func configureMainMenuPromosUpdateNeeded(c *Config, h *api0.Handler) error {
	switch typ, arg, _ := strings.Cut(c.API0_MainMenuPromos_UpdateNeeded, ":"); typ {
	case "none":
		return nil
	case "file":
		p, err := filepath.Abs(arg)
		if err != nil {
			return fmt.Errorf("file: resolve %q: %w", arg, err)
		}
		fn1 := h.MainMenuPromos
		h.MainMenuPromos = func(r *http.Request) api0.MainMenuPromos {
			var mmp api0.MainMenuPromos
			if fn1 != nil {
				mmp = fn1(r)
			}
			if r == nil || !h.CheckLauncherVersion(r, true) {
				if buf, err1 := os.ReadFile(p); err1 != nil {
					err = err1
				} else if err = json.Unmarshal(buf, &mmp); err != nil {
					err = err1
				}
			}
			return mmp
		}
		if h.MainMenuPromos(nil); err != nil {
			return fmt.Errorf("file: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown source %q", typ)
	}
}

func configureIP2Location(c *Config) (*ip2xMgr, error) {
	if c.IP2Location == "" {
		return nil, nil
	}
	mgr := new(ip2xMgr)
	return mgr, mgr.Load(c.IP2Location)
}

func configureRegionMap(c *Config) (fn func(netip.Addr, ip2x.Record) (string, error), err error) {
	switch m := c.API0_RegionMap; m {
	case "", "none":
		fn = nil
	case "default":
		fn = regionmap.GetRegion
	default:
		return nil, fmt.Errorf("unknown region map type %q", m)
	}
	if len(c.API0_RegionMap_Override) != 0 {
		type regionMapOverride struct {
			Prefix netip.Prefix
			Region string
		}
		var mos []regionMapOverride
		for _, x := range c.API0_RegionMap_Override {
			a, r, ok := strings.Cut(x, "=")
			if !ok {
				return nil, fmt.Errorf("parse region override %q: missing equals sign", x)
			}
			if strings.ContainsRune(a, '/') {
				if pfx, err := netip.ParsePrefix(a); err == nil {
					mos = append(mos, regionMapOverride{pfx, r})
				} else {
					return nil, fmt.Errorf("parse region override %q: invalid prefix: %w", x, err)
				}
			} else {
				if x, err := netip.ParseAddr(a); err == nil {
					if pfx, err := x.Prefix(x.BitLen()); err == nil {
						mos = append(mos, regionMapOverride{pfx, r})
					} else {
						panic(err)
					}
				} else {
					return nil, fmt.Errorf("parse region override %q: invalid prefix: %w", x, err)
				}
			}
		}
		next := fn
		fn = func(a netip.Addr, r ip2x.Record) (string, error) {
			for _, mo := range mos {
				if mo.Prefix.Contains(a) {
					return mo.Region, nil
				}
			}
			return next(a, r)
		}
	}
	return
}

// Run runs the server, shutting it down gracefully when ctx is canceled, then
// waiting indefinitely for it to exit. It must only ever be called once, and
// the server is useless afterwards.
func (s *Server) Run(ctx context.Context) error {
	if s.closed {
		return http.ErrServerClosed
	}

	go func() {
		tk := time.NewTicker(time.Minute * 5)
		defer tk.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				s.API0.ServerList.ReapServers()
			}
		}
	}()

	var hs []*http.Server
	var as []string
	for _, a := range s.Addr {
		hs = append(hs, &http.Server{
			Addr:    a,
			Handler: s.Handler,
		})
		as = append(as, "http://"+a)
	}
	for _, a := range s.AddrTLS {
		hs = append(hs, &http.Server{
			Addr:      a,
			Handler:   s.Handler,
			TLSConfig: s.TLSConfig,
		})
		as = append(as, "https://"+a)
	}
	if len(hs) == 0 {
		return fmt.Errorf("no listen addresses provided")
	}
	s.Logger.Log().Msgf("starting server on %s", strings.Join(as, ", "))

	errch := make(chan error, len(hs))
	for _, h := range hs {
		h := h
		go func() {
			if h.TLSConfig != nil {
				errch <- h.ListenAndServeTLS("", "")
			} else {
				errch <- h.ListenAndServe()
			}
		}()
	}
	go func() {
		errch <- s.API0.NSPkt.ListenAndServe(netip.AddrPort{})
	}()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second * 2):
		go s.sdnotify("READY=1")
	case err := <-errch:
		s.Logger.Err(err).Msg("failed to start server")
		return err
	}

	select {
	case <-ctx.Done():
		s.closed = true
		s.Logger.Log().Msg("shutting down")

		go s.sdnotify("STOPPING=1")

		var wg sync.WaitGroup
		for _, h := range hs {
			h := h
			wg.Add(1)
			go func() {
				h.Shutdown(ctx)
				wg.Done()
			}()
		}
		wg.Wait()

		if c, ok := s.API0.AccountStorage.(io.Closer); ok {
			c.Close()
		}
		if c, ok := s.API0.PdataStorage.(io.Closer); ok {
			c.Close()
		}
		return nil
	case err := <-errch:
		s.Logger.Err(err).Msg("failed to start server")
		return err
	}
}

func (s *Server) HandleSIGHUP() {
	if s.closed {
		return
	}

	s.sdnotify("RELOADING=1")
	defer s.sdnotify("READY=1")

	for _, fn := range s.reload {
		if fn != nil {
			fn()
		}
	}
}

// serveRest handles endpoints not handled by the API.
func (s *Server) serveRest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/metrics" {
		var internal, geo bool
		if s := s.MetricsSecret; s != "" {
			if r.URL.Query().Get("secret") == s {
				internal = true
			}
		}
		geo = r.URL.Query().Has("geo")

		var ms []func(io.Writer)
		if internal {
			ms = append(ms, metrics.WriteProcessMetrics)
			ms = append(ms, s.API0.WritePrometheus)
			ms = append(ms, s.API0.NSPkt.WritePrometheus)
		}
		ms = append(ms, s.API0.ServerList.WritePrometheus)
		if internal && geo {
			ms = append(ms, s.API0.WritePrometheusGeo)
			ms = append(ms, s.API0.ServerList.WritePrometheusGeo)
		}

		var b bytes.Buffer
		for i, m := range ms {
			if i != 0 {
				b.WriteByte('\n')
			}
			m(&b)
		}

		w.Header().Set("Cache-Control", "private, no-cache, no-store")
		w.Header().Set("Expires", "0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
		w.WriteHeader(http.StatusOK)
		b.WriteTo(w)
		return
	}

	if s.Web != nil {
		s.Web.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Cache-Control", "private, no-cache, no-store")
	w.Header().Set("Expires", "0")
	w.Header().Set("Pragma", "no-cache")

	if r.URL.Path == "/" {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "Go away.\n")
		return
	}

	http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
}

func (s *Server) sdnotify(state string) (bool, error) {
	if s.NotifySocket == "" {
		return false, nil
	}

	socketAddr := &net.UnixAddr{
		Name: s.NotifySocket,
		Net:  "unixgram",
	}

	conn, err := net.DialUnix(socketAddr.Net, nil, socketAddr)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(state)); err != nil {
		return false, err
	}
	return true, nil
}
