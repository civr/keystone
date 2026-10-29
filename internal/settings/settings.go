// Package settings holds Keystone's tunable security settings.
package settings

// Settings is the full set of GUI-editable security settings.
type Settings struct {
	Domain     string             `json:"domain"`     // parent domain, e.g. home.lan
	PublicURL  string             `json:"public_url"` // e.g. https://keystone.home.lan
	Session    SessionSettings    `json:"session"`
	Login      LoginSettings      `json:"login"`
	BruteForce BruteForceSettings `json:"brute_force"`
	Network    NetworkSettings    `json:"network"`
	Headers    HeaderSettings     `json:"headers"`
	Audit      AuditSettings      `json:"audit"`
	Tokens     TokenSettings      `json:"tokens"`
	LDAP       LDAPSettings       `json:"ldap"`
	Gate       GateSettings       `json:"gate"`
	Vault      VaultSettings      `json:"vault"`
	ProxyAuth  ProxyAuthSettings  `json:"proxy_auth"`
}

// ProxyAuthSettings let a reverse proxy that already signed the person in
// (an identity-aware proxy) sign them in to Keystone with a header. The
// header counts only when the request reached Keystone directly from one
// of Trusted (the address Caddy saw).
type ProxyAuthSettings struct {
	Enabled bool     `json:"enabled"`
	Header  string   `json:"header"`
	Match   string   `json:"match"` // username or email
	Trusted []string `json:"trusted"`
}

type SessionSettings struct {
	DurationHours      int    `json:"duration_hours"`
	TrustedDeviceDays  int    `json:"trusted_device_days"`
	IdleTimeoutMinutes int    `json:"idle_timeout_minutes"`
	Sliding            bool   `json:"sliding"`
	SameSite           string `json:"same_site"`
}

type LoginSettings struct {
	PasskeyRequired            bool   `json:"passkey_required"`
	PasskeyOnlyOnTrustedDevice bool   `json:"passkey_only_on_trusted_device"`
	UserVerification           string `json:"user_verification"`
	StepUpMinutes              int    `json:"step_up_minutes"`
}

type BruteForceSettings struct {
	LoginPerIPPerMinute      int `json:"login_per_ip_per_minute"`
	LoginPerAccountPerMinute int `json:"login_per_account_per_minute"`
	LockoutThreshold         int `json:"lockout_threshold"`
	LockoutMinutes           int `json:"lockout_minutes"`
}

type NetworkSettings struct {
	LoginCIDRs     []string `json:"login_cidrs"`
	AdminCIDRs     []string `json:"admin_cidrs"`
	TrustedProxies []string `json:"trusted_proxies"`
}

type HeaderSettings struct {
	HSTS                  bool   `json:"hsts"`
	HSTSIncludeSubdomains bool   `json:"hsts_include_subdomains"`
	HSTSPreload           bool   `json:"hsts_preload"`
	FrameDeny             bool   `json:"frame_deny"`
	HSTSMaxAge            int    `json:"hsts_max_age"`
	CSPExtra              string `json:"csp_extra"`
}

type AuditSettings struct {
	RetentionDays int    `json:"retention_days"`
	SyslogEnabled bool   `json:"syslog_enabled"`
	SyslogAddr    string `json:"syslog_addr"`
	SyslogProto   string `json:"syslog_proto"`
}

// TokenSettings are OIDC token lifetimes.
type TokenSettings struct {
	AccessMinutes int `json:"access_minutes"`
	IDMinutes     int `json:"id_minutes"`
	RefreshDays   int `json:"refresh_days"` // 0 = no refresh tokens
}

// LDAPSettings govern keystone-ldap.
type LDAPSettings struct {
	RequireTLS         bool     `json:"require_tls"`
	AllowedCIDRs       []string `json:"allowed_cidrs"`
	BindPerIPPerMinute int      `json:"bind_per_ip_per_minute"`
}

// GateSettings govern keystone-gate.
type GateSettings struct {
	FailPerIPPerMinute int `json:"fail_per_ip_per_minute"`
}

// VaultSettings govern the credential vault.
type VaultSettings struct {
	ReconfirmMinutes      int  `json:"reconfirm_minutes"`
	ClipboardClearSeconds int  `json:"clipboard_clear_seconds"`
	AllowReveal           bool `json:"allow_reveal"`
}

// Defaults returns the factory settings.
func Defaults() Settings {
	return Settings{
		Session:    SessionSettings{DurationHours: 720, TrustedDeviceDays: 90, IdleTimeoutMinutes: 0, Sliding: true, SameSite: "Lax"},
		Login:      LoginSettings{PasskeyRequired: true, PasskeyOnlyOnTrustedDevice: true, UserVerification: "required", StepUpMinutes: 5},
		BruteForce: BruteForceSettings{LoginPerIPPerMinute: 10, LoginPerAccountPerMinute: 5, LockoutThreshold: 10, LockoutMinutes: 15},
		Network:    NetworkSettings{LoginCIDRs: []string{}, AdminCIDRs: []string{}, TrustedProxies: []string{}},
		Headers:    HeaderSettings{HSTS: true, HSTSIncludeSubdomains: true, HSTSPreload: false, FrameDeny: true, HSTSMaxAge: 31536000},
		Audit:      AuditSettings{RetentionDays: 365, SyslogEnabled: false, SyslogAddr: "", SyslogProto: "udp"},
		Tokens:     TokenSettings{AccessMinutes: 10, IDMinutes: 10, RefreshDays: 30},
		LDAP:       LDAPSettings{RequireTLS: true, AllowedCIDRs: []string{}, BindPerIPPerMinute: 10},
		Gate:       GateSettings{FailPerIPPerMinute: 60},
		Vault:      VaultSettings{ReconfirmMinutes: 0, ClipboardClearSeconds: 30, AllowReveal: true},
		ProxyAuth:  ProxyAuthSettings{Header: "Remote-User", Match: "username", Trusted: []string{}},
	}
}

// normalize makes nil lists empty so they serialize as [] rather than null.
func (s *Settings) normalize() {
	if s.Network.LoginCIDRs == nil {
		s.Network.LoginCIDRs = []string{}
	}
	if s.Network.AdminCIDRs == nil {
		s.Network.AdminCIDRs = []string{}
	}
	if s.Network.TrustedProxies == nil {
		s.Network.TrustedProxies = []string{}
	}
	if s.LDAP.AllowedCIDRs == nil {
		s.LDAP.AllowedCIDRs = []string{}
	}
	if s.ProxyAuth.Trusted == nil {
		s.ProxyAuth.Trusted = []string{}
	}
}
