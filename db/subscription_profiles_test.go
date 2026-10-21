package db

import "testing"

func TestSubscriptionProfilesAndAPIKeyMigration(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer d.Close()
	id, err := d.SaveProfile(&LLMProfile{Name: "subscription-test", Format: "openai", Model: "test", APIKey: "api-key-original", Streaming: true})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM llm_profiles WHERE id=$1`, id)
	p, _ := d.ProfileByID(id)
	p.APIKey = "api-key-replacement"
	if _, err := d.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	p, _ = d.ProfileByID(id)
	if p.APIKey != "api-key-replacement" {
		t.Fatal("API key update regressed")
	}
	for _, format := range []string{"chatgpt", "claude-code"} {
		p.Format = format
		p.APIKey = ""
		if _, err := d.SaveProfile(p); err != nil {
			t.Fatal(err)
		}
		p, _ = d.ProfileByID(id)
		if p.APIKey != "" || p.Format != format {
			t.Fatal("subscription kept API key")
		}
		all, err := d.ListProfiles()
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range all {
			if item.ID == id && item.APIKeyHint != "" {
				t.Fatal("stale API key hint")
			}
		}
		pool, err := d.PoolProfiles()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range pool {
			if item.ID == id {
				found = true
			}
		}
		if !found {
			t.Fatal("keyless subscription omitted from pool")
		}
	}
	p.Format = "openai"
	p.APIKey = ""
	if _, err := d.SaveProfile(p); err != nil {
		t.Fatal(err)
	}
	pool, err := d.PoolProfiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range pool {
		if item.ID == id {
			t.Fatal("empty API key profile entered pool")
		}
	}
}
