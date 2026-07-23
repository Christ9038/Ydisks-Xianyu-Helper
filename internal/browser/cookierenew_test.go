package browser

import (
	"testing"

	"xianyu-go/internal/xianyu/cookierefresh"
)

func TestCredentialCookieSnapshotUsesCurrentFlatCookieAsAuthority(t *testing.T) {
	existing := []cookierefresh.BrowserCookie{
		{Name: "session", Value: "stale", Domain: ".goofish.com", Path: "/", Expires: 123, HTTPOnly: true, Secure: true, SameSite: "Lax"},
		{Name: "removed", Value: "old", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "passport", Value: "keep", Domain: ".taobao.com", Path: "/", Secure: true},
	}
	got := credentialCookieSnapshot(existing, map[string]string{
		"session": "fresh",
		"new":     "value",
	})
	if len(got) != 3 {
		t.Fatalf("snapshot len=%d, want 3: %+v", len(got), got)
	}
	byName := make(map[string]cookierefresh.BrowserCookie, len(got))
	for _, cookie := range got {
		byName[cookie.Name] = cookie
	}
	session := byName["session"]
	if session.Value != "fresh" || session.Expires != 123 || !session.HTTPOnly || session.SameSite != "Lax" {
		t.Fatalf("existing attributes or fresh value were lost: %+v", session)
	}
	if _, exists := byName["removed"]; exists {
		t.Fatalf("cookie absent from current flat value was resurrected: %+v", got)
	}
	if passport := byName["passport"]; passport.Value != "keep" || passport.Domain != ".taobao.com" {
		t.Fatalf("out-of-scope cookie was lost: %+v", passport)
	}
	added := byName["new"]
	if added.Value != "value" || added.Domain != goofishDot || added.Path != "/" || !added.Secure {
		t.Fatalf("new cookie defaults mismatch: %+v", added)
	}
}

func TestCredentialCookieSnapshotPreservesAmbiguousSameNameScopes(t *testing.T) {
	existing := []cookierefresh.BrowserCookie{
		{Name: "token", Value: "old-root", Domain: ".goofish.com", Path: "/"},
		{Name: "token", Value: "old-im", Domain: "www.goofish.com", Path: "/im", HTTPOnly: true},
	}
	got := credentialCookieSnapshot(existing, map[string]string{"token": "fresh"})
	if len(got) != 2 {
		t.Fatalf("same-name scoped cookies collapsed: %+v", got)
	}
	values := map[string]string{}
	for _, cookie := range got {
		values[cookie.Path] = cookie.Value
	}
	if values["/"] != "old-root" || values["/im"] != "old-im" {
		t.Fatalf("ambiguous flat value corrupted scoped cookies: %+v", got)
	}
}

func TestReconcileSnapshotWithCurrentCookie(t *testing.T) {
	snapshot := []cookierefresh.BrowserCookie{{Name: "session", Value: "old", Domain: ".goofish.com", Path: "/", Expires: 456}}

	got := reconcileSnapshotWithCurrentCookie(snapshot, "session=new", goofishIMURL)
	if len(got) != 1 || got[0].Value != "new" || got[0].Expires != 456 {
		t.Fatalf("flat cookie did not update snapshot without losing attributes: %+v", got)
	}

	got = reconcileSnapshotWithCurrentCookie(snapshot, "", goofishIMURL)
	if len(got) != 1 || got[0].Value != "old" {
		t.Fatalf("snapshot-only renewal input was discarded: %+v", got)
	}

	got = reconcileSnapshotWithCurrentCookie([]cookierefresh.BrowserCookie{}, "session=stale", goofishIMURL)
	if got == nil || len(got) != 0 {
		t.Fatalf("authoritative empty snapshot must not fall back to flat cookies: %#v", got)
	}
	if got := reconcileSnapshotWithCurrentCookie(nil, "session=legacy", goofishIMURL); got != nil {
		t.Fatalf("missing snapshot should preserve the legacy flat-cookie fallback: %#v", got)
	}
}

func TestSilentHasLoginURLMatcher(t *testing.T) {
	if !isSilentHasLoginURL("https://passport.goofish.com/newlogin/silentHasLogin.do?appName=xianyu") {
		t.Fatal("official silentHasLogin request was not recognized")
	}
	if isSilentHasLoginURL("https://passport.goofish.com/newlogin/hasLogin.do") {
		t.Fatal("ordinary hasLogin request must not be treated as silentHasLogin")
	}
}
