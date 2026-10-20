package db

import "testing"

func rule(kind, pattern string, enabled bool) AssetInterceptRule {
	return AssetInterceptRule{Kind: kind, Pattern: pattern, Enabled: enabled}
}

func TestMatchAssetInterceptRules(t *testing.T) {
	cases := []struct {
		name    string
		rules   []AssetInterceptRule
		domains []string
		ips     []string
		urls    []string
		want    bool
		wantVal string
	}{
		{"built-in fuzzy government domain matches", []AssetInterceptRule{rule("fuzzy_domain", ".gov.cn", true)},
			[]string{"www.beijing.gov.cn"}, nil, nil, true, "www.beijing.gov.cn"},
		{"fuzzy education domain matches", []AssetInterceptRule{rule("fuzzy_domain", ".edu", true)},
			[]string{"mit.edu"}, nil, nil, true, "mit.edu"},
		{"exact domain match ignores case", []AssetInterceptRule{rule("exact_domain", "Example.com", true)},
			[]string{"example.com"}, nil, nil, true, "example.com"},
		{"exact domain does not match subdomain", []AssetInterceptRule{rule("exact_domain", "example.com", true)},
			[]string{"a.example.com"}, nil, nil, false, ""},
		{"exact IP matches", []AssetInterceptRule{rule("exact_ip", "203.0.113.5", true)},
			nil, []string{"203.0.113.5"}, nil, true, "203.0.113.5"},
		{"fuzzy IP prefix matches", []AssetInterceptRule{rule("fuzzy_ip", "203.0.113.", true)},
			nil, []string{"203.0.113.99"}, nil, true, "203.0.113.99"},
		{"CIDR matches", []AssetInterceptRule{rule("cidr", "192.168.0.0/16", true)},
			nil, []string{"192.168.5.20"}, nil, true, "192.168.5.20"},
		{"CIDR does not match", []AssetInterceptRule{rule("cidr", "192.168.0.0/16", true)},
			nil, []string{"10.0.0.1"}, nil, false, ""},
		{"exact URL matches", []AssetInterceptRule{rule("exact_url", "https://a.gov.cn/login", true)},
			nil, nil, []string{"https://a.gov.cn/login"}, true, "https://a.gov.cn/login"},
		{"fuzzy URL matches path", []AssetInterceptRule{rule("fuzzy_url", "/admin", true)},
			nil, nil, []string{"https://x.com/admin/panel"}, true, "https://x.com/admin/panel"},
		{"disabled rule does not match", []AssetInterceptRule{rule("fuzzy_domain", ".gov.cn", false)},
			[]string{"www.gov.cn"}, nil, nil, false, ""},
		{"no rules do not match", nil, []string{"www.gov.cn"}, nil, nil, false, ""},
		{"empty pattern does not match", []AssetInterceptRule{rule("fuzzy_domain", "  ", true)},
			[]string{"www.gov.cn"}, nil, nil, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, val, ok := MatchAssetInterceptRules(c.rules, c.domains, c.ips, c.urls)
			if ok != c.want {
				t.Fatalf("matched = %v, want %v (rule=%+v)", ok, c.want, r)
			}
			if ok && val != c.wantVal {
				t.Fatalf("matched value = %q, want %q", val, c.wantVal)
			}
		})
	}
}

func TestEvaluateAssetGate(t *testing.T) {
	block := []AssetInterceptRule{rule("fuzzy_domain", ".gov.cn", true)}
	allow := []AssetInterceptRule{rule("fuzzy_domain", "example.com", true)}

	// 1. A matching intercept rule denies access (intercept reason takes priority).
	if d := EvaluateAssetGate(block, allow, []string{"www.gov.cn"}, nil, nil); d.Allowed {
		t.Fatal("matching intercept rule should deny access")
	}

	// 2. An allow rule exists but does not match: deny access.
	d := EvaluateAssetGate(block, allow, []string{"foo.other.com"}, nil, nil)
	if d.Allowed {
		t.Fatal("nonmatching allowlist should deny access")
	}
	if d.Reason == "" {
		t.Fatal("denial should include a reason")
	}

	// 3. No intercept matches and an allow rule matches: allow access.
	if d := EvaluateAssetGate(block, allow, []string{"api.example.com"}, nil, nil); !d.Allowed {
		t.Fatal("matching allowlist should allow access")
	}

	// 4. No allow rules means the allowlist is disabled: allow if no intercept matches.
	if d := EvaluateAssetGate(block, nil, []string{"foo.other.com"}, nil, nil); !d.Allowed {
		t.Fatal("without an allowlist, nonmatching intercept should allow access")
	}

	// 5. All allow rules disabled means the allowlist is disabled: allow access.
	disabledAllow := []AssetInterceptRule{rule("fuzzy_domain", "example.com", false)}
	if d := EvaluateAssetGate(nil, disabledAllow, []string{"foo.other.com"}, nil, nil); !d.Allowed {
		t.Fatal("disabled allowlist should allow access")
	}

	// 6. Intercept wins if the same target matches both intercept and allow rules.
	if d := EvaluateAssetGate(
		[]AssetInterceptRule{rule("fuzzy_domain", ".gov.cn", true)},
		[]AssetInterceptRule{rule("fuzzy_domain", ".gov.cn", true)},
		[]string{"www.gov.cn"}, nil, nil,
	); d.Allowed {
		t.Fatal("intercept should take priority over allow")
	}
}

func TestAssetInterceptCandidates(t *testing.T) {
	// For a service asset with only a URL, extract the host as a domain candidate
	// so fuzzy_domain can match it.
	a := &Asset{Type: "service", URL: "https://portal.beijing.gov.cn:8443/app"}
	domains, _, urls := a.interceptCandidates()
	if len(urls) != 1 || urls[0] != a.URL {
		t.Fatalf("urls = %v", urls)
	}
	found := false
	for _, d := range domains {
		if d == "portal.beijing.gov.cn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("URL host was not extracted as a domain candidate: %v", domains)
	}
	r, _, ok := MatchAssetInterceptRules([]AssetInterceptRule{rule("fuzzy_domain", ".gov.cn", true)}, domains, nil, urls)
	if !ok {
		t.Fatalf("URL-only government service should match fuzzy_domain, rule=%+v", r)
	}

	// If the URL host is an IP, include it as an IP candidate for CIDR matching.
	b := &Asset{Type: "service", URL: "http://10.1.2.3/x"}
	_, ips, _ := b.interceptCandidates()
	if r, _, ok := MatchAssetInterceptRules([]AssetInterceptRule{rule("cidr", "10.0.0.0/8", true)}, nil, ips, nil); !ok {
		t.Fatalf("IP in URL should match CIDR, ips=%v rule=%+v", ips, r)
	}
}
