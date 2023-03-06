package api0

import (
	"fmt"
	"io"
	"reflect"

	"github.com/VictoriaMetrics/metrics"
	"github.com/r2northstar/atlas/pkg/metricsx"
)

// note: for results, fail_ prefix is for errors which are likely a problem with the backend, and reject_ are for client errors

type apiMetrics struct {
	set                      *metrics.Set
	request_panics_total     *metrics.Counter
	versiongate_checks_total struct {
		success_ok     *metrics.Counter
		success_dev    *metrics.Counter
		reject_old     *metrics.Counter
		reject_invalid *metrics.Counter
		reject_notns   *metrics.Counter
	}
	accounts_writepersistence_extradata_size_bytes *metrics.Histogram // only includes successful updates
	accounts_writepersistence_stored_size_bytes    *metrics.Histogram
	accounts_writepersistence_requests_total       struct {
		success                    *metrics.Counter
		reject_too_much_extradata  *metrics.Counter
		reject_too_large           *metrics.Counter
		reject_invalid_pdata       *metrics.Counter
		reject_bad_request         *metrics.Counter
		reject_player_not_found    *metrics.Counter
		reject_unauthorized        *metrics.Counter
		fail_storage_error_account *metrics.Counter
		fail_storage_error_pdata   *metrics.Counter
		fail_other_error           *metrics.Counter
		http_method_not_allowed    *metrics.Counter
	}
	accounts_lookupuid_requests_total struct {
		success_singlematch        *metrics.Counter
		success_multimatch         *metrics.Counter
		success_nomatch            *metrics.Counter
		reject_bad_request         *metrics.Counter
		fail_storage_error_account *metrics.Counter
		http_method_not_allowed    *metrics.Counter
	}
	accounts_getusername_requests_total struct {
		success_match              *metrics.Counter
		success_missing            *metrics.Counter
		reject_bad_request         *metrics.Counter
		reject_player_not_found    *metrics.Counter
		fail_storage_error_account *metrics.Counter
		http_method_not_allowed    *metrics.Counter
	}
	client_mainmenupromos_requests_total struct {
		success                 func(version string) *metrics.Counter
		http_method_not_allowed *metrics.Counter
	}
	client_mainmenupromos_requests_map *metricsx.GeoCounter2
	client_originauth_requests_total   struct {
		success                     *metrics.Counter
		reject_bad_request          *metrics.Counter
		reject_versiongate          *metrics.Counter
		reject_stryder_invalidgame  *metrics.Counter
		reject_stryder_invalidtoken *metrics.Counter
		reject_stryder_mpnotallowed *metrics.Counter
		reject_stryder_other        *metrics.Counter
		fail_storage_error_account  *metrics.Counter
		fail_stryder_error          *metrics.Counter
		fail_other_error            *metrics.Counter
		http_method_not_allowed     *metrics.Counter
	}
	client_originauth_requests_map                            *metricsx.GeoCounter2
	client_originauth_stryder_auth_duration_seconds           *metrics.Histogram
	client_originauth_origin_username_lookup_duration_seconds *metrics.Histogram
	client_originauth_origin_username_lookup_calls_total      struct {
		success              *metrics.Counter
		notfound             *metrics.Counter
		fail_authtok_refresh *metrics.Counter
		fail_other_error     *metrics.Counter
	}
	client_originauth_eax_username_lookup_duration_seconds *metrics.Histogram
	client_originauth_eax_username_lookup_calls_total      struct {
		success           *metrics.Counter
		notfound          *metrics.Counter
		fail_update_check *metrics.Counter
		fail_other_error  *metrics.Counter
	}
	client_authwithserver_requests_total struct {
		success                    *metrics.Counter
		reject_bad_request         *metrics.Counter
		reject_versiongate         *metrics.Counter
		reject_player_not_found    *metrics.Counter
		reject_masterserver_token  *metrics.Counter
		reject_password            *metrics.Counter
		reject_gameserverauth      *metrics.Counter
		reject_gameserver          *metrics.Counter
		fail_gameserverauth        *metrics.Counter
		fail_gameserverauthudp     *metrics.Counter
		fail_storage_error_account *metrics.Counter
		fail_storage_error_pdata   *metrics.Counter
		fail_other_error           *metrics.Counter
		http_method_not_allowed    *metrics.Counter
	}
	client_authwithserver_gameserverauth_duration_seconds    *metrics.Histogram
	client_authwithserver_gameserverauthudp_duration_seconds *metrics.Histogram
	client_authwithserver_gameserverauthudp_attempts         *metrics.Histogram
	client_authwithself_requests_total                       struct {
		success                    *metrics.Counter
		reject_bad_request         *metrics.Counter
		reject_versiongate         *metrics.Counter
		reject_player_not_found    *metrics.Counter
		reject_masterserver_token  *metrics.Counter
		fail_storage_error_account *metrics.Counter
		fail_storage_error_pdata   *metrics.Counter
		fail_other_error           *metrics.Counter
		http_method_not_allowed    *metrics.Counter
	}
	client_servers_requests_total struct {
		success                 func(version string) *metrics.Counter
		http_method_not_allowed *metrics.Counter
	}
	client_servers_requests_map struct {
		northstar *metricsx.GeoCounter2
		other     *metricsx.GeoCounter2
	}
	client_servers_response_size_bytes struct {
		gzip *metrics.Histogram
		none *metrics.Histogram
	}
	server_upsert_requests_total struct {
		success_updated            func(action string) *metrics.Counter
		success_verified           func(action string) *metrics.Counter
		reject_versiongate         func(action string) *metrics.Counter
		reject_ipv6                func(action string) *metrics.Counter
		reject_bad_request         func(action string) *metrics.Counter
		reject_unauthorized_ip     func(action string) *metrics.Counter
		reject_server_not_found    func(action string) *metrics.Counter
		reject_duplicate_auth_addr func(action string) *metrics.Counter
		reject_limits_exceeded     func(action string) *metrics.Counter
		reject_verify_authtimeout  func(action string) *metrics.Counter
		reject_verify_authresp     func(action string) *metrics.Counter
		reject_verify_autherr      func(action string) *metrics.Counter
		reject_verify_udptimeout   func(action string) *metrics.Counter
		reject_verify_udperr       func(action string) *metrics.Counter
		fail_other_error           func(action string) *metrics.Counter
		fail_serverlist_error      func(action string) *metrics.Counter
		http_method_not_allowed    func(action string) *metrics.Counter
	}
	server_upsert_modinfo_parse_errors_total func(action string) *metrics.Counter
	server_upsert_verify_time_seconds        struct {
		success *metrics.Histogram
		failure *metrics.Histogram
	}
	server_upsert_ip2location_errors_total *metrics.Counter
	server_upsert_getregion_errors_total   *metrics.Counter
	server_remove_requests_total           struct {
		success                 *metrics.Counter
		reject_unauthorized_ip  *metrics.Counter
		reject_bad_request      *metrics.Counter
		reject_server_not_found *metrics.Counter
		fail_other_error        *metrics.Counter
		http_method_not_allowed *metrics.Counter
	}
	server_connect_requests_total struct {
		success                         *metrics.Counter
		success_reject                  *metrics.Counter
		success_pdata                   *metrics.Counter
		reject_unauthorized_ip          *metrics.Counter
		reject_server_not_found         *metrics.Counter
		reject_invalid_connection_token *metrics.Counter
		reject_must_get_pdata           *metrics.Counter
		reject_bad_request              *metrics.Counter
		fail_other_error                *metrics.Counter
		http_method_not_allowed         *metrics.Counter
	}
	player_pdata_requests_total struct {
		success                  func(filter string) *metrics.Counter
		reject_bad_request       *metrics.Counter
		reject_player_not_found  *metrics.Counter
		fail_storage_error_pdata *metrics.Counter
		fail_pdata_invalid       *metrics.Counter
		fail_other_error         *metrics.Counter
		http_method_not_allowed  *metrics.Counter
	}
}

func (h *Handler) Metrics() *metrics.Set {
	return h.m().set
}

func (h *Handler) WritePrometheus(w io.Writer) {
	h.m().set.WritePrometheus(w)
}

func (h *Handler) WritePrometheusGeo(w io.Writer) {
	h.m().client_mainmenupromos_requests_map.WritePrometheus(w)
	h.m().client_originauth_requests_map.WritePrometheus(w)
	h.m().client_servers_requests_map.northstar.WritePrometheus(w)
	h.m().client_servers_requests_map.other.WritePrometheus(w)
}

// m gets metrics objects for h.
//
// We use it instead of using a *metrics.Set directly because:
//   - It means we don't need to keep checking if a set is nil.
//   - It means we don't have the overhead of checking/creating each individual metric during requests.
//   - It makes typos less likely.
//   - It means that metrics still get included in the output instead of being undefined even if they start at zero.
func (h *Handler) m() *apiMetrics {
	h.metricsInit.Do(func() {
		mo := &h.metricsObj
		mo.set = metrics.NewSet()
		mo.request_panics_total = mo.set.NewCounter(`atlas_api0_request_panics_total`)
		mo.versiongate_checks_total.success_ok = mo.set.NewCounter(`atlas_api0_versiongate_checks_total{result="success_ok"}`)
		mo.versiongate_checks_total.success_dev = mo.set.NewCounter(`atlas_api0_versiongate_checks_total{result="success_dev"}`)
		mo.versiongate_checks_total.reject_old = mo.set.NewCounter(`atlas_api0_versiongate_checks_total{result="reject_old"}`)
		mo.versiongate_checks_total.reject_invalid = mo.set.NewCounter(`atlas_api0_versiongate_checks_total{result="reject_invalid"}`)
		mo.versiongate_checks_total.reject_notns = mo.set.NewCounter(`atlas_api0_versiongate_checks_total{result="reject_notns"}`)
		mo.accounts_writepersistence_extradata_size_bytes = mo.set.NewHistogram(`atlas_api0_accounts_writepersistence_extradata_size_bytes`)
		mo.accounts_writepersistence_stored_size_bytes = mo.set.NewHistogram(`atlas_api0_accounts_writepersistence_stored_size_bytes`)
		mo.accounts_writepersistence_requests_total.success = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="success"}`)
		mo.accounts_writepersistence_requests_total.reject_too_much_extradata = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="reject_too_much_extradata"}`)
		mo.accounts_writepersistence_requests_total.reject_too_large = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="reject_too_large"}`)
		mo.accounts_writepersistence_requests_total.reject_invalid_pdata = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="reject_invalid_pdata"}`)
		mo.accounts_writepersistence_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="reject_bad_request"}`)
		mo.accounts_writepersistence_requests_total.reject_player_not_found = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="reject_player_not_found"}`)
		mo.accounts_writepersistence_requests_total.reject_unauthorized = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="reject_unauthorized"}`)
		mo.accounts_writepersistence_requests_total.fail_storage_error_account = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="fail_storage_error_account"}`)
		mo.accounts_writepersistence_requests_total.fail_storage_error_pdata = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="fail_storage_error_pdata"}`)
		mo.accounts_writepersistence_requests_total.fail_other_error = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="fail_other_error"}`)
		mo.accounts_writepersistence_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_accounts_writepersistence_requests_total{result="http_method_not_allowed"}`)
		mo.accounts_lookupuid_requests_total.success_singlematch = mo.set.NewCounter(`atlas_api0_accounts_lookupuid_requests_total{result="success_singlematch"}`)
		mo.accounts_lookupuid_requests_total.success_multimatch = mo.set.NewCounter(`atlas_api0_accounts_lookupuid_requests_total{result="success_multimatch"}`)
		mo.accounts_lookupuid_requests_total.success_nomatch = mo.set.NewCounter(`atlas_api0_accounts_lookupuid_requests_total{result="success_nomatch"}`)
		mo.accounts_lookupuid_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_accounts_lookupuid_requests_total{result="reject_bad_request"}`)
		mo.accounts_lookupuid_requests_total.fail_storage_error_account = mo.set.NewCounter(`atlas_api0_accounts_lookupuid_requests_total{result="fail_storage_error_account"}`)
		mo.accounts_lookupuid_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_accounts_lookupuid_requests_total{result="http_method_not_allowed"}`)
		mo.accounts_getusername_requests_total.success_match = mo.set.NewCounter(`atlas_api0_accounts_getusername_requests_total{result="success_match"}`)
		mo.accounts_getusername_requests_total.success_missing = mo.set.NewCounter(`atlas_api0_accounts_getusername_requests_total{result="success_missing"}`)
		mo.accounts_getusername_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_accounts_getusername_requests_total{result="reject_bad_request"}`)
		mo.accounts_getusername_requests_total.reject_player_not_found = mo.set.NewCounter(`atlas_api0_accounts_getusername_requests_total{result="reject_player_not_found"}`)
		mo.accounts_getusername_requests_total.fail_storage_error_account = mo.set.NewCounter(`atlas_api0_accounts_getusername_requests_total{result="fail_storage_error_account"}`)
		mo.accounts_getusername_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_accounts_getusername_requests_total{result="http_method_not_allowed"}`)
		mo.client_mainmenupromos_requests_total.success = func(launcher_version string) *metrics.Counter {
			if launcher_version == "" {
				launcher_version = "unknown"
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_client_mainmenupromos_requests_total{result="success",launcher_version="` + launcher_version + `"}`)
		}
		mo.client_mainmenupromos_requests_total.success("unknown")
		mo.client_mainmenupromos_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_client_servers_response_size_bytes{result="http_method_not_allowed"}`)
		mo.client_mainmenupromos_requests_map = metricsx.NewGeoCounter2(`atlas_api0_client_mainmenupromos_requests_map`)
		mo.client_originauth_requests_total.success = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="success"}`)
		mo.client_originauth_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="reject_bad_request"}`)
		mo.client_originauth_requests_total.reject_versiongate = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="reject_versiongate"}`)
		mo.client_originauth_requests_total.reject_stryder_invalidgame = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="reject_stryder_invalidgame"}`)
		mo.client_originauth_requests_total.reject_stryder_invalidtoken = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="reject_stryder_invalidtoken"}`)
		mo.client_originauth_requests_total.reject_stryder_mpnotallowed = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="reject_stryder_mpnotallowed"}`)
		mo.client_originauth_requests_total.reject_stryder_other = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="reject_stryder_other"}`)
		mo.client_originauth_requests_total.fail_storage_error_account = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="fail_storage_error_account"}`)
		mo.client_originauth_requests_total.fail_stryder_error = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="fail_stryder_error"}`)
		mo.client_originauth_requests_total.fail_other_error = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="fail_other_error"}`)
		mo.client_originauth_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_client_originauth_requests_total{result="http_method_not_allowed"}`)
		mo.client_originauth_requests_map = metricsx.NewGeoCounter2(`atlas_api0_client_originauth_requests_map`)
		mo.client_originauth_stryder_auth_duration_seconds = mo.set.NewHistogram(`atlas_api0_client_originauth_stryder_auth_duration_seconds`)
		mo.client_originauth_origin_username_lookup_duration_seconds = mo.set.NewHistogram(`atlas_api0_client_originauth_origin_username_lookup_duration_seconds`)
		mo.client_originauth_origin_username_lookup_calls_total.success = mo.set.NewCounter(`atlas_api0_client_originauth_origin_username_lookup_calls_total{result="success"}`)
		mo.client_originauth_origin_username_lookup_calls_total.notfound = mo.set.NewCounter(`atlas_api0_client_originauth_origin_username_lookup_calls_total{result="notfound"}`)
		mo.client_originauth_origin_username_lookup_calls_total.fail_authtok_refresh = mo.set.NewCounter(`atlas_api0_client_originauth_origin_username_lookup_calls_total{result="fail_authtok_refresh"}`)
		mo.client_originauth_origin_username_lookup_calls_total.fail_other_error = mo.set.NewCounter(`atlas_api0_client_originauth_origin_username_lookup_calls_total{result="fail_other_error"}`)
		mo.client_originauth_eax_username_lookup_duration_seconds = mo.set.NewHistogram(`atlas_api0_client_originauth_eax_username_lookup_duration_seconds`)
		mo.client_originauth_eax_username_lookup_calls_total.success = mo.set.NewCounter(`atlas_api0_client_originauth_eax_username_lookup_calls_total{result="success"}`)
		mo.client_originauth_eax_username_lookup_calls_total.notfound = mo.set.NewCounter(`atlas_api0_client_originauth_eax_username_lookup_calls_total{result="notfound"}`)
		mo.client_originauth_eax_username_lookup_calls_total.fail_update_check = mo.set.NewCounter(`atlas_api0_client_originauth_eax_username_lookup_calls_total{result="fail_update_check"}`)
		mo.client_originauth_eax_username_lookup_calls_total.fail_other_error = mo.set.NewCounter(`atlas_api0_client_originauth_eax_username_lookup_calls_total{result="fail_other_error"}`)
		mo.client_authwithserver_requests_total.success = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="success"}`)
		mo.client_authwithserver_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="reject_bad_request"}`)
		mo.client_authwithserver_requests_total.reject_versiongate = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="reject_versiongate"}`)
		mo.client_authwithserver_requests_total.reject_player_not_found = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="reject_player_not_found"}`)
		mo.client_authwithserver_requests_total.reject_masterserver_token = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="reject_masterserver_token"}`)
		mo.client_authwithserver_requests_total.reject_password = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="reject_password"}`)
		mo.client_authwithserver_requests_total.reject_gameserverauth = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="reject_gameserverauth"}`)
		mo.client_authwithserver_requests_total.reject_gameserver = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="reject_gameserver"}`)
		mo.client_authwithserver_requests_total.fail_gameserverauth = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="fail_gameserverauth"}`)
		mo.client_authwithserver_requests_total.fail_gameserverauthudp = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="fail_gameserverauthudp"}`)
		mo.client_authwithserver_requests_total.fail_storage_error_account = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="fail_storage_error_account"}`)
		mo.client_authwithserver_requests_total.fail_storage_error_pdata = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="fail_storage_error_pdata"}`)
		mo.client_authwithserver_requests_total.fail_other_error = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="fail_other_error"}`)
		mo.client_authwithserver_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_client_authwithserver_requests_total{result="http_method_not_allowed"}`)
		mo.client_authwithserver_gameserverauth_duration_seconds = mo.set.NewHistogram(`atlas_api0_client_authwithserver_gameserverauth_duration_seconds`)
		mo.client_authwithserver_gameserverauthudp_duration_seconds = mo.set.NewHistogram(`atlas_api0_client_authwithserver_gameserverauthudp_duration_seconds`)
		mo.client_authwithserver_gameserverauthudp_attempts = mo.set.NewHistogram(`atlas_api0_client_authwithserver_gameserverauthudp_attempts`)
		mo.client_authwithself_requests_total.success = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="success"}`)
		mo.client_authwithself_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="reject_bad_request"}`)
		mo.client_authwithself_requests_total.reject_versiongate = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="reject_versiongate"}`)
		mo.client_authwithself_requests_total.reject_player_not_found = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="reject_player_not_found"}`)
		mo.client_authwithself_requests_total.reject_masterserver_token = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="reject_masterserver_token"}`)
		mo.client_authwithself_requests_total.fail_storage_error_account = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="fail_storage_error_account"}`)
		mo.client_authwithself_requests_total.fail_storage_error_pdata = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="fail_storage_error_pdata"}`)
		mo.client_authwithself_requests_total.fail_other_error = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="fail_other_error"}`)
		mo.client_authwithself_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_client_authwithself_requests_total{result="http_method_not_allowed"}`)
		mo.client_servers_requests_total.success = func(launcher_version string) *metrics.Counter {
			if launcher_version == "" {
				launcher_version = "unknown"
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_client_servers_requests_total{result="success",launcher_version="` + launcher_version + `"}`)
		}
		mo.client_servers_requests_total.success("unknown")
		mo.client_servers_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_client_servers_requests_total{result="http_method_not_allowed"}`)
		mo.client_servers_requests_map.northstar = metricsx.NewGeoCounter2(`atlas_api0_client_servers_requests_map{user_agent="northstar"}`)
		mo.client_servers_requests_map.other = metricsx.NewGeoCounter2(`atlas_api0_client_servers_requests_map{user_agent="other"}`)
		mo.client_servers_response_size_bytes.gzip = mo.set.NewHistogram(`atlas_api0_client_servers_response_size_bytes{compression="gzip"}`)
		mo.client_servers_response_size_bytes.none = mo.set.NewHistogram(`atlas_api0_client_servers_response_size_bytes{compression="none"}`)
		mo.server_upsert_requests_total.success_updated = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="success_updated",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.success_verified = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="success_verified",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_versiongate = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_versiongate",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_ipv6 = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_ipv6",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_bad_request = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_bad_request",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_unauthorized_ip = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_unauthorized_ip",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_server_not_found = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_server_not_found",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_duplicate_auth_addr = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_duplicate_auth_addr",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_limits_exceeded = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_limits_exceeded",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_verify_authtimeout = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_verify_authtimeout",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_verify_authresp = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_verify_authresp",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_verify_autherr = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_verify_autherr",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_verify_udptimeout = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_verify_udptimeout",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.reject_verify_udperr = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="reject_verify_udperr",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.fail_other_error = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="fail_other_error",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.fail_serverlist_error = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="fail_serverlist_error",action="` + action + `"}`)
		}
		mo.server_upsert_requests_total.http_method_not_allowed = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_requests_total{result="http_method_not_allowed",action="` + action + `"}`)
		}
		mo.server_upsert_modinfo_parse_errors_total = func(action string) *metrics.Counter {
			if action == "" {
				panic("invalid action")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_server_upsert_modinfo_parse_errors_total{action="` + action + `"}`)
		}
		for _, action := range []string{"add_server", "update_values", "heartbeat"} {
			mo.server_upsert_requests_total.success_updated(action)
			mo.server_upsert_requests_total.success_verified(action)
			mo.server_upsert_requests_total.reject_versiongate(action)
			mo.server_upsert_requests_total.reject_ipv6(action)
			mo.server_upsert_requests_total.reject_bad_request(action)
			mo.server_upsert_requests_total.reject_unauthorized_ip(action)
			mo.server_upsert_requests_total.reject_server_not_found(action)
			mo.server_upsert_requests_total.reject_duplicate_auth_addr(action)
			mo.server_upsert_requests_total.reject_limits_exceeded(action)
			mo.server_upsert_requests_total.reject_verify_authtimeout(action)
			mo.server_upsert_requests_total.reject_verify_authresp(action)
			mo.server_upsert_requests_total.reject_verify_autherr(action)
			mo.server_upsert_requests_total.reject_verify_udptimeout(action)
			mo.server_upsert_requests_total.reject_verify_udperr(action)
			mo.server_upsert_requests_total.fail_other_error(action)
			mo.server_upsert_requests_total.fail_serverlist_error(action)
			mo.server_upsert_requests_total.http_method_not_allowed(action)
			mo.server_upsert_modinfo_parse_errors_total(action)
		}
		mo.server_upsert_verify_time_seconds.success = mo.set.NewHistogram(`atlas_api0_server_upsert_verify_time_seconds{success="true"}`)
		mo.server_upsert_verify_time_seconds.failure = mo.set.NewHistogram(`atlas_api0_server_upsert_verify_time_seconds{success="false"}`)
		mo.server_upsert_ip2location_errors_total = mo.set.NewCounter(`atlas_api0_server_upsert_ip2location_errors_total`)
		mo.server_upsert_getregion_errors_total = mo.set.NewCounter(`atlas_api0_server_upsert_getregion_errors_total`)
		mo.server_remove_requests_total.success = mo.set.NewCounter(`atlas_api0_server_remove_requests_total{result="success"}`)
		mo.server_remove_requests_total.reject_unauthorized_ip = mo.set.NewCounter(`atlas_api0_server_remove_requests_total{result="reject_unauthorized_ip"}`)
		mo.server_remove_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_server_remove_requests_total{result="reject_bad_request"}`)
		mo.server_remove_requests_total.reject_server_not_found = mo.set.NewCounter(`atlas_api0_server_remove_requests_total{result="reject_server_not_found"}`)
		mo.server_remove_requests_total.fail_other_error = mo.set.NewCounter(`atlas_api0_server_remove_requests_total{result="fail_other_error"}`)
		mo.server_remove_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_server_remove_requests_total{result="http_method_not_allowed"}`)
		mo.server_connect_requests_total.success = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="success"}`)
		mo.server_connect_requests_total.success_reject = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="success_reject"}`)
		mo.server_connect_requests_total.success_pdata = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="success_pdata"}`)
		mo.server_connect_requests_total.reject_unauthorized_ip = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="reject_unauthorized_ip"}`)
		mo.server_connect_requests_total.reject_server_not_found = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="reject_server_not_found"}`)
		mo.server_connect_requests_total.reject_invalid_connection_token = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="reject_invalid_connection_token"}`)
		mo.server_connect_requests_total.reject_must_get_pdata = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="reject_must_get_pdata"}`)
		mo.server_connect_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="reject_bad_request"}`)
		mo.server_connect_requests_total.fail_other_error = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="fail_other_error"}`)
		mo.server_connect_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_server_connect_requests_total{result="http_method_not_allowed"}`)
		mo.player_pdata_requests_total.success = func(filter string) *metrics.Counter {
			if filter == "" {
				panic("invalid filter")
			}
			return mo.set.GetOrCreateCounter(`atlas_api0_player_pdata_requests_total{result="success",filter="` + filter + `"}`)
		}
		mo.player_pdata_requests_total.reject_bad_request = mo.set.NewCounter(`atlas_api0_player_pdata_requests_total{result="reject_bad_request"}`)
		mo.player_pdata_requests_total.reject_player_not_found = mo.set.NewCounter(`atlas_api0_player_pdata_requests_total{result="reject_player_not_found"}`)
		mo.player_pdata_requests_total.fail_storage_error_pdata = mo.set.NewCounter(`atlas_api0_player_pdata_requests_total{result="fail_storage_error_pdata"}`)
		mo.player_pdata_requests_total.fail_pdata_invalid = mo.set.NewCounter(`atlas_api0_player_pdata_requests_total{result="fail_pdata_invalid"}`)
		mo.player_pdata_requests_total.fail_other_error = mo.set.NewCounter(`atlas_api0_player_pdata_requests_total{result="fail_other_error"}`)
		mo.player_pdata_requests_total.http_method_not_allowed = mo.set.NewCounter(`atlas_api0_player_pdata_requests_total{result="http_method_not_allowed"}`)
	})

	// ensure we initialized everything
	var chk func(v reflect.Value, name string)
	chk = func(v reflect.Value, name string) {
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				chk(v.Field(i), name+"."+v.Type().Field(i).Name)
			}
		case reflect.Pointer, reflect.Func:
			if v.IsNil() {
				panic(fmt.Errorf("check metrics: unexpected nil %q", name))
			}
		default:
			panic(fmt.Errorf("check metrics: unexpected kind %s", v.Kind()))
		}
	}
	chk(reflect.ValueOf(h.metricsObj), "metricsObj")

	return &h.metricsObj
}
