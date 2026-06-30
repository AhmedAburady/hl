package caddy

import "testing"

func TestParseSites_HostsAndUpstreams(t *testing.T) {
	content := "a.home.lab {\n\treverse_proxy http://10.0.0.1:80\n}\n\nb.home.lab {\n\treverse_proxy http://10.0.0.2:80\n}\n"
	sites, err := ParseSites(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 2 || sites[0].Host != "a.home.lab" || sites[1].Host != "b.home.lab" {
		t.Fatalf("got %+v", sites)
	}
	if sites[0].Upstream != "http://10.0.0.1:80" {
		t.Errorf("upstream wrong: %q", sites[0].Upstream)
	}
	if sites[0].DNS.Present {
		t.Errorf("unannotated block should have no DNS directive")
	}
}

func TestParseSites_UpstreamForms(t *testing.T) {
	cases := map[string]string{
		"bare":       "a.home.lab {\n\treverse_proxy 10.0.0.1:80\n}\n",
		"trailing {": "a.home.lab {\n\treverse_proxy 10.0.0.1:80 {\n\t\theader_up Host {host}\n\t}\n}\n",
		"block to":   "a.home.lab {\n\treverse_proxy {\n\t\tto 10.0.0.1:80\n\t}\n}\n",
		"scheme":     "a.home.lab {\n\treverse_proxy http://10.0.0.1:80\n}\n",
	}
	for name, content := range cases {
		sites, err := ParseSites(content)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(sites) != 1 {
			t.Fatalf("%s: got %d sites", name, len(sites))
		}
		want := "10.0.0.1:80"
		if name == "scheme" {
			want = "http://10.0.0.1:80"
		}
		if sites[0].Upstream != want {
			t.Errorf("%s: upstream = %q, want %q", name, sites[0].Upstream, want)
		}
	}
}

func TestParseSites_IgnoresNestedBlocks(t *testing.T) {
	content := "app.home.lab {\n\thandle /api/* {\n\t\treverse_proxy http://10.0.0.1:80\n\t}\n}\n"
	sites, err := ParseSites(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 1 || sites[0].Host != "app.home.lab" {
		t.Fatalf("got %+v", sites)
	}
}
