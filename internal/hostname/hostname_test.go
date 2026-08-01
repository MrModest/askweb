package hostname

import (
	"testing"
)

func TestCanonicalStripsPort(t *testing.T) {
	got, err := Canonical("https://example.com:8443/path")
	if err != nil {
		t.Fatalf("Canonical returned error: %v", err)
	}
	if got != "example.com" {
		t.Errorf("Canonical = %q, want %q", got, "example.com")
	}
}

func TestCanonicalRejects(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
	}{
		{"no host", "https:///just/a/path"},
		{"empty string", ""},
		{"bare path", "/etc/passwd"},
		{"scheme-less host", "example.com/path"},
		{"ftp scheme", "ftp://example.com/file"},
		{"file scheme", "file://example.com/etc/passwd"},
		{"javascript scheme", "javascript:alert(1)"},
		{"data scheme", "data:text/html,hello"},
		{"uppercase non-http scheme", "FTP://example.com/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Canonical(tt.rawURL)
			if err == nil {
				t.Errorf("Canonical(%q) = %q, want error", tt.rawURL, got)
			}
		})
	}
}

func TestCanonicalAcceptsHTTPAndHTTPS(t *testing.T) {
	for _, rawURL := range []string{"http://example.com/", "https://example.com/", "HTTPS://example.com/"} {
		got, err := Canonical(rawURL)
		if err != nil {
			t.Errorf("Canonical(%q) returned error: %v", rawURL, err)
			continue
		}
		if got != "example.com" {
			t.Errorf("Canonical(%q) = %q, want %q", rawURL, got, "example.com")
		}
	}
}

func TestCanonicalHostIsIdempotentOnAlreadyCanonicalNames(t *testing.T) {
	for _, host := range []string{"example.com", "go.dev", "xn--mnchen-3ya.de", "a.b.example.com"} {
		got, err := CanonicalHost(host)
		if err != nil {
			t.Errorf("CanonicalHost(%q) returned error: %v", host, err)
			continue
		}
		if got != host {
			t.Errorf("CanonicalHost(%q) = %q, want it unchanged", host, got)
		}
	}
}

func TestCanonicalHostNormalizesAndRejects(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		want    string
		wantErr bool
	}{
		{name: "uppercase", host: "Example.COM", want: "example.com"},
		{name: "unicode", host: "münchen.de", want: "xn--mnchen-3ya.de"},
		{name: "empty", host: "", wantErr: true},
		{name: "with port", host: "example.com:8443", wantErr: true},
		{name: "full URL", host: "https://example.com/", wantErr: true},
		{name: "leading space", host: " example.com", wantErr: true},
		{name: "with path", host: "example.com/path", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalHost(tt.host)
			if tt.wantErr {
				if err == nil {
					t.Errorf("CanonicalHost(%q) = %q, want error", tt.host, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CanonicalHost(%q) returned error: %v", tt.host, err)
			}
			if got != tt.want {
				t.Errorf("CanonicalHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestCanonicalLowercasesHost(t *testing.T) {
	got, err := Canonical("https://Example.COM/path")
	if err != nil {
		t.Fatalf("Canonical returned error: %v", err)
	}
	if got != "example.com" {
		t.Errorf("Canonical = %q, want %q", got, "example.com")
	}
}

func TestCanonicalConvertsUnicodeHostToPunycode(t *testing.T) {
	got, err := Canonical("https://münchen.de/")
	if err != nil {
		t.Fatalf("Canonical returned error: %v", err)
	}
	if got != "xn--mnchen-3ya.de" {
		t.Errorf("Canonical = %q, want %q", got, "xn--mnchen-3ya.de")
	}
}

// A Cyrillic "а" (U+0430) renders identically to ASCII "a" but must canonicalize
// to a different string, so a homograph can never satisfy a whitelist entry.
func TestCanonicalDistinguishesHomographFromASCIILookalike(t *testing.T) {
	homograph, err := Canonical("https://аpple.com/")
	if err != nil {
		t.Fatalf("Canonical returned error: %v", err)
	}
	ascii, err := Canonical("https://apple.com/")
	if err != nil {
		t.Fatalf("Canonical returned error: %v", err)
	}
	if homograph == ascii {
		t.Fatalf("homograph and ASCII host both canonicalized to %q", homograph)
	}
	if homograph != "xn--pple-43d.com" {
		t.Errorf("Canonical = %q, want %q", homograph, "xn--pple-43d.com")
	}
}
