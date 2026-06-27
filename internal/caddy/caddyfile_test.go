package caddy

import (
	"strings"
	"testing"
)

func TestUpsertReverseProxy_InsertNew(t *testing.T) {
	content := ""
	out, res, err := UpsertReverseProxy(content, "app.home.lab", "http://192.168.1.50:8080", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Created || !res.Changed {
		t.Fatalf("expected created+changed, got %+v", res)
	}
	want := "app.home.lab {\n\treverse_proxy http://192.168.1.50:8080\n}\n"
	if out != want {
		t.Fatalf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestUpsertReverseProxy_UpdateExisting(t *testing.T) {
	content := "app.home.lab {\n\treverse_proxy http://10.0.0.1:80\n}\n"
	out, res, err := UpsertReverseProxy(content, "app.home.lab", "http://192.168.1.50:8080", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Updated || !res.Changed {
		t.Fatalf("expected updated+changed, got %+v", res)
	}
	if strings.Contains(out, "10.0.0.1:80") {
		t.Fatalf("old upstream not replaced: %s", out)
	}
	if !strings.Contains(out, "reverse_proxy http://192.168.1.50:8080") {
		t.Fatalf("new upstream missing: %s", out)
	}
}

func TestUpsertReverseProxy_Idempotent(t *testing.T) {
	content := "app.home.lab {\n\treverse_proxy http://192.168.1.50:8080\n}\n"
	out, res, err := UpsertReverseProxy(content, "app.home.lab", "http://192.168.1.50:8080", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Changed {
		t.Fatalf("expected no change, got %+v; out=%s", res, out)
	}
}

func TestUpsertReverseProxy_PreservesOtherDirectives(t *testing.T) {
	content := "app.home.lab {\n\tencode gzip\n\treverse_proxy http://10.0.0.1:80\n\ttls internal\n}\n"
	out, res, err := UpsertReverseProxy(content, "app.home.lab", "http://192.168.1.50:8080", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Changed {
		t.Fatalf("expected change")
	}
	for _, want := range []string{"encode gzip", "tls internal", "reverse_proxy http://192.168.1.50:8080"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "10.0.0.1") {
		t.Fatalf("old upstream present:\n%s", out)
	}
}

func TestUpsertReverseProxy_ImportsInsertIntoExistingBlock(t *testing.T) {
	content := "app.home.lab {\n\tencode gzip\n}\n"
	out, res, err := UpsertReverseProxy(content, "app.home.lab", "http://192.168.1.50:8080", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Updated || !res.Changed {
		t.Fatalf("expected updated+changed, got %+v", res)
	}
	if !strings.Contains(out, "reverse_proxy http://192.168.1.50:8080") {
		t.Fatalf("reverse_proxy not inserted:\n%s", out)
	}
	if !strings.Contains(out, "encode gzip") {
		t.Fatalf("encode directive lost:\n%s", out)
	}
}

func TestUpsertReverseProxy_BlockFormErrorsWithoutForce(t *testing.T) {
	content := "app.home.lab {\n\treverse_proxy {\n\t\tto 10.0.0.1:80\n\t}\n}\n"
	_, _, err := UpsertReverseProxy(content, "app.home.lab", "http://192.168.1.50:8080", false)
	if err == nil {
		t.Fatalf("expected error for block form without force")
	}
}

func TestUpsertReverseProxy_BlockFormForceReplaces(t *testing.T) {
	content := "app.home.lab {\n\treverse_proxy {\n\t\tto 10.0.0.1:80\n\t}\n}\n"
	out, res, err := UpsertReverseProxy(content, "app.home.lab", "http://192.168.1.50:8080", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Changed {
		t.Fatalf("expected change")
	}
	if strings.Contains(out, "to 10.0.0.1") {
		t.Fatalf("old block not replaced:\n%s", out)
	}
	if !strings.Contains(out, "reverse_proxy http://192.168.1.50:8080") {
		t.Fatalf("new directive missing:\n%s", out)
	}
}

func TestUpsertReverseProxy_MultipleHostsSelectsRightBlock(t *testing.T) {
	content := "a.home.lab {\n\treverse_proxy http://10.0.0.1:80\n}\n\nb.home.lab {\n\treverse_proxy http://10.0.0.2:80\n}\n"
	out, res, err := UpsertReverseProxy(content, "b.home.lab", "http://192.168.1.99:8080", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Updated {
		t.Fatalf("expected update")
	}
	if !strings.Contains(out, "a.home.lab {\n\treverse_proxy http://10.0.0.1:80") {
		t.Fatalf("unrelated block modified:\n%s", out)
	}
	if !strings.Contains(out, "b.home.lab {\n\treverse_proxy http://192.168.1.99:8080") {
		t.Fatalf("target block not updated:\n%s", out)
	}
}

func TestUpsertReverseProxy_HostKeyOnOwnLine(t *testing.T) {
	content := "app.home.lab\n{\n\treverse_proxy http://10.0.0.1:80\n}\n"
	out, res, err := UpsertReverseProxy(content, "app.home.lab", "http://192.168.1.50:8080", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Updated || !res.Changed {
		t.Fatalf("expected updated+changed, got %+v", res)
	}
	if !strings.Contains(out, "reverse_proxy http://192.168.1.50:8080") {
		t.Fatalf("not updated:\n%s", out)
	}
}

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
