package sing

import (
	"encoding/json"
	"math"
	"net/url"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	"github.com/sagernet/sing-box/option"
)

func baseSingOptions() *conf.Options {
	return &conf.Options{
		ListenIP:    "0.0.0.0",
		SingOptions: &conf.SingOptions{},
	}
}

func websocketNetworkSettings(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := json.Marshal(WsNetworkConfig{Path: path})
	if err != nil {
		t.Fatalf("marshal WsNetworkConfig: %v", err)
	}
	return raw
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint16
		wantErr bool
	}{
		{name: "min_valid", input: "1", want: 1},
		{name: "normal", input: "443", want: 443},
		{name: "leading_zeros", input: "000443", want: 443},
		{name: "plus_sign", input: "+443", want: 443},
		{name: "max_valid", input: "65535", want: math.MaxUint16},
		{name: "zero_rejected", input: "0", wantErr: true},
		{name: "negative_rejected", input: "-1", wantErr: true},
		{name: "overflow_rejected", input: "65536", wantErr: true},
		{name: "large_overflow_rejected", input: "4294967296", wantErr: true},
		{name: "empty_rejected", input: "", wantErr: true},
		{name: "space_padded_rejected", input: " 443", wantErr: true},
		{name: "hex_rejected", input: "0x1bb", wantErr: true},
		{name: "non_numeric_rejected", input: "abc", wantErr: true},
		{name: "decimal_rejected", input: "443.0", wantErr: true},
		{name: "int64_overflow_rejected", input: "99999999999999999999", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePort(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parsePort(%q) = %d, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePort(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parsePort(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseEarlyData(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint32
		wantErr bool
	}{
		{name: "zero_means_disabled", input: "0", want: 0},
		{name: "min_non_zero", input: "1", want: 1},
		{name: "normal", input: "2048", want: 2048},
		{name: "above_port_range_is_valid", input: "65536", want: 65536},
		{name: "max_uint32", input: "4294967295", want: math.MaxUint32},
		{name: "negative_rejected", input: "-1", wantErr: true},
		{name: "max_plus_one_rejected", input: "4294967296", wantErr: true},
		{name: "large_overflow_rejected", input: "18446744073709551616", wantErr: true},
		{name: "empty_rejected", input: "", wantErr: true},
		{name: "space_padded_rejected", input: " 2048", wantErr: true},
		{name: "non_numeric_rejected", input: "abc", wantErr: true},
		{name: "decimal_rejected", input: "12.5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEarlyData(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseEarlyData(%q) = %d, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEarlyData(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parseEarlyData(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseEarlyDataQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   url.Values
		want    uint32
		wantErr bool
	}{
		{name: "absent_keeps_old_default", query: nil, want: 0},
		{name: "empty_keeps_old_default", query: url.Values{"ed": {""}}, want: 0},
		{name: "zero", query: url.Values{"ed": {"0"}}, want: 0},
		{name: "normal", query: url.Values{"ed": {"2048"}}, want: 2048},
		{name: "first_value_wins", query: url.Values{"ed": {"2048", "0"}}, want: 2048},
		{name: "max_uint32", query: url.Values{"ed": {"4294967295"}}, want: math.MaxUint32},
		{name: "negative_rejected", query: url.Values{"ed": {"-1"}}, wantErr: true},
		{name: "overflow_rejected", query: url.Values{"ed": {"4294967296"}}, wantErr: true},
		{name: "non_numeric_rejected", query: url.Values{"ed": {"abc"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEarlyDataQuery(tt.query)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseEarlyDataQuery(%v) = %d, want error", tt.query, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEarlyDataQuery(%v) unexpected error: %v", tt.query, err)
			}
			if got != tt.want {
				t.Fatalf("parseEarlyDataQuery(%v) = %d, want %d", tt.query, got, tt.want)
			}
		})
	}
}

func TestGetInboundOptionsRealityServerPort(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		want    uint16
		wantErr bool
	}{
		{name: "normal", port: "443", want: 443},
		{name: "min_valid", port: "1", want: 1},
		{name: "max_valid", port: "65535", want: math.MaxUint16},
		{name: "empty_rejected", port: "", wantErr: true},
		{name: "zero_rejected", port: "0", wantErr: true},
		{name: "negative_rejected", port: "-1", wantErr: true},
		{name: "overflow_rejected", port: "65536", wantErr: true},
		{name: "non_numeric_rejected", port: "abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &panel.NodeInfo{
				Type:     "vless",
				Security: panel.Reality,
				Common:   &panel.CommonNode{ServerPort: 443},
				VAllss:   &panel.VAllssNode{Network: "tcp"},
			}
			info.VAllss.TlsSettings.ServerName = "www.example.com"
			info.VAllss.TlsSettings.ServerPort = tt.port

			in, err := getInboundOptions("test", info, baseSingOptions())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("getInboundOptions with server_port %q: want error", tt.port)
				}
				return
			}
			if err != nil {
				t.Fatalf("getInboundOptions with server_port %q: unexpected error: %v", tt.port, err)
			}
			opts, ok := in.Options.(*option.VLESSInboundOptions)
			if !ok {
				t.Fatalf("unexpected options type %T", in.Options)
			}
			if opts.TLS == nil || opts.TLS.Reality == nil {
				t.Fatal("reality options not built")
			}
			if got := opts.TLS.Reality.Handshake.ServerOptions.ServerPort; got != tt.want {
				t.Fatalf("reality server port = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetInboundOptionsWebsocketEarlyData(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    uint32
		wantErr bool
	}{
		{name: "absent_uses_zero", path: "/ws", want: 0},
		{name: "empty_query_uses_zero", path: "/ws?ed=", want: 0},
		{name: "zero_means_disabled", path: "/ws?ed=0", want: 0},
		{name: "normal", path: "/ws?ed=2048", want: 2048},
		{name: "above_port_range_is_valid", path: "/ws?ed=65536", want: 65536},
		{name: "max_uint32", path: "/ws?ed=4294967295", want: math.MaxUint32},
		{name: "negative_rejected", path: "/ws?ed=-1", wantErr: true},
		{name: "overflow_rejected", path: "/ws?ed=4294967296", wantErr: true},
		{name: "non_numeric_rejected", path: "/ws?ed=abc", wantErr: true},
		{name: "decimal_rejected", path: "/ws?ed=1.5", wantErr: true},
	}
	t.Run("vmess", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				info := &panel.NodeInfo{
					Type:   "vmess",
					Common: &panel.CommonNode{ServerPort: 443},
					VAllss: &panel.VAllssNode{
						Network:         "ws",
						NetworkSettings: websocketNetworkSettings(t, tt.path),
					},
				}
				in, err := getInboundOptions("test", info, baseSingOptions())
				if tt.wantErr {
					if err == nil {
						t.Fatalf("getInboundOptions path %q: want error", tt.path)
					}
					return
				}
				if err != nil {
					t.Fatalf("getInboundOptions path %q: unexpected error: %v", tt.path, err)
				}
				opts, ok := in.Options.(*option.VMessInboundOptions)
				if !ok {
					t.Fatalf("unexpected options type %T", in.Options)
				}
				if got := opts.Transport.WebsocketOptions.MaxEarlyData; got != tt.want {
					t.Fatalf("vmess MaxEarlyData = %d, want %d", got, tt.want)
				}
			})
		}
	})
	t.Run("trojan", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				info := &panel.NodeInfo{
					Type:   "trojan",
					Common: &panel.CommonNode{ServerPort: 443},
					Trojan: &panel.TrojanNode{
						Network:         "ws",
						NetworkSettings: websocketNetworkSettings(t, tt.path),
					},
				}
				in, err := getInboundOptions("test", info, baseSingOptions())
				if tt.wantErr {
					if err == nil {
						t.Fatalf("getInboundOptions path %q: want error", tt.path)
					}
					return
				}
				if err != nil {
					t.Fatalf("getInboundOptions path %q: unexpected error: %v", tt.path, err)
				}
				opts, ok := in.Options.(*option.TrojanInboundOptions)
				if !ok {
					t.Fatalf("unexpected options type %T", in.Options)
				}
				if got := opts.Transport.WebsocketOptions.MaxEarlyData; got != tt.want {
					t.Fatalf("trojan MaxEarlyData = %d, want %d", got, tt.want)
				}
			})
		}
	})
}

func TestGetInboundOptionsTrojanFallback(t *testing.T) {
	tests := []struct {
		name         string
		fallback     *conf.FallBackConfigForSing
		wantErr      bool
		wantPort     uint16
		wantALPNPort uint16
	}{
		{name: "nil_config_no_fallback", fallback: nil},
		{name: "empty_default_no_fallback", fallback: &conf.FallBackConfigForSing{}},
		{
			name: "fallback_valid",
			fallback: &conf.FallBackConfigForSing{
				FallBack: conf.FallBack{Server: "127.0.0.1", ServerPort: "8443"},
			},
			wantPort: 8443,
		},
		{
			name: "fallback_zero_rejected",
			fallback: &conf.FallBackConfigForSing{
				FallBack: conf.FallBack{Server: "127.0.0.1", ServerPort: "0"},
			},
			wantErr: true,
		},
		{
			name: "fallback_negative_rejected",
			fallback: &conf.FallBackConfigForSing{
				FallBack: conf.FallBack{Server: "127.0.0.1", ServerPort: "-1"},
			},
			wantErr: true,
		},
		{
			name: "fallback_overflow_rejected",
			fallback: &conf.FallBackConfigForSing{
				FallBack: conf.FallBack{Server: "127.0.0.1", ServerPort: "65536"},
			},
			wantErr: true,
		},
		{
			name: "fallback_non_numeric_rejected",
			fallback: &conf.FallBackConfigForSing{
				FallBack: conf.FallBack{Server: "127.0.0.1", ServerPort: "abc"},
			},
			wantErr: true,
		},
		{
			name: "fallback_server_without_port_rejected",
			fallback: &conf.FallBackConfigForSing{
				FallBack: conf.FallBack{Server: "127.0.0.1"},
			},
			wantErr: true,
		},
		{
			name: "fallback_for_alpn_valid",
			fallback: &conf.FallBackConfigForSing{
				FallBackForALPN: map[string]conf.FallBack{
					"h2": {Server: "127.0.0.1", ServerPort: "8443"},
				},
			},
			wantALPNPort: 8443,
		},
		{
			name: "fallback_for_alpn_zero_rejected",
			fallback: &conf.FallBackConfigForSing{
				FallBackForALPN: map[string]conf.FallBack{
					"h2": {Server: "127.0.0.1", ServerPort: "0"},
				},
			},
			wantErr: true,
		},
		{
			name: "fallback_for_alpn_overflow_rejected",
			fallback: &conf.FallBackConfigForSing{
				FallBackForALPN: map[string]conf.FallBack{
					"h2": {Server: "127.0.0.1", ServerPort: "70000"},
				},
			},
			wantErr: true,
		},
		{
			name: "fallback_for_alpn_non_numeric_rejected",
			fallback: &conf.FallBackConfigForSing{
				FallBackForALPN: map[string]conf.FallBack{
					"h2": {Server: "127.0.0.1", ServerPort: "abc"},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseSingOptions()
			c.SingOptions.FallBackConfigs = tt.fallback
			info := &panel.NodeInfo{
				Type:   "trojan",
				Common: &panel.CommonNode{ServerPort: 443},
				Trojan: &panel.TrojanNode{Network: "tcp"},
			}
			in, err := getInboundOptions("test", info, c)
			if tt.wantErr {
				if err == nil {
					t.Fatal("getInboundOptions: want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("getInboundOptions: unexpected error: %v", err)
			}
			opts, ok := in.Options.(*option.TrojanInboundOptions)
			if !ok {
				t.Fatalf("unexpected options type %T", in.Options)
			}
			if tt.wantPort != 0 {
				if opts.Fallback == nil {
					t.Fatal("fallback not built")
				}
				if got := opts.Fallback.ServerPort; got != tt.wantPort {
					t.Fatalf("fallback server port = %d, want %d", got, tt.wantPort)
				}
			} else if opts.Fallback != nil {
				t.Fatalf("unexpected fallback: %+v", opts.Fallback)
			}
			if tt.wantALPNPort != 0 {
				server, ok := opts.FallbackForALPN["h2"]
				if !ok || server == nil {
					t.Fatal("fallback for alpn h2 not built")
				}
				if got := server.ServerPort; got != tt.wantALPNPort {
					t.Fatalf("fallbackForALPN server port = %d, want %d", got, tt.wantALPNPort)
				}
			} else if len(opts.FallbackForALPN) != 0 {
				t.Fatalf("unexpected fallback for alpn: %+v", opts.FallbackForALPN)
			}
		})
	}
}
