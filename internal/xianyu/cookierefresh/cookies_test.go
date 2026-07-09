package cookierefresh

import "testing"

func TestMetadataSnapshotKeyCompatibility(t *testing.T) {
	oldMeta := `{"cookie_refresh_snapshot":[{"name":"a","value":"1","domain":".goofish.com","path":"/"}]}`
	snapshot := SnapshotFromMetadata(oldMeta)
	if len(snapshot) != 1 || snapshot[0].Name != "a" {
		t.Fatalf("旧 key 快照读取失败: %+v", snapshot)
	}
	newMeta := MetadataWithSnapshot(oldMeta, []BrowserCookie{{Name: "b", Value: "2", Domain: ".taobao.com", Path: "/"}})
	if SnapshotFromMetadata(newMeta)[0].Name != "b" {
		t.Fatalf("新 key 快照写入失败: %s", newMeta)
	}
	if got := MetadataWithoutSnapshot(newMeta); len(SnapshotFromMetadata(got)) != 0 {
		t.Fatalf("快照应被清除: %s", got)
	}
}

func TestChangedSnapshotLabels(t *testing.T) {
	before := []BrowserCookie{{Name: "a", Value: "1", Domain: ".goofish.com", Path: "/"}}
	after := []BrowserCookie{{Name: "a", Value: "2", Domain: ".goofish.com", Path: "/"}}
	got := ChangedSnapshotLabels(before, after)
	if len(got) != 1 || got[0] != "a@.goofish.com/" {
		t.Fatalf("ChangedSnapshotLabels=%v", got)
	}
}
