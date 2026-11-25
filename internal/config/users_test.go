package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSystemUsersFromFiltersAndSortsNonRootUsers(t *testing.T) {
	tmp := t.TempDir()
	homeAlice := filepath.Join(tmp, "alice")
	homeBob := filepath.Join(tmp, "bob")
	homeDeploy := filepath.Join(tmp, "deploy")
	for _, d := range []string{homeAlice, homeBob, homeDeploy} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	passwdContent := "" +
		"root:x:0:0:root:/root:/bin/bash\n" +
		"daemon:x:2:2:daemon:/sbin:/usr/sbin/nologin\n" +
		"nginx:x:969:969:Nginx:/var/lib/nginx:/sbin/nologin\n" +
		"bob:x:1002:1002:Bob:" + homeBob + ":/bin/zsh\n" +
		"alice:x:1000:1000:Alice:" + homeAlice + ":/bin/bash\n" +
		"deploy:x:1001:1001:Deploy:" + homeDeploy + ":/bin/bash\n" +
		"lockeduser:x:1003:1003:Locked:" + homeAlice + ":/usr/sbin/nologin\n" +
		"missinghome:x:1004:1004:Missing:" + filepath.Join(tmp, "missing") + ":/bin/bash\n" +
		"nobody:x:65534:65534:Nobody:/:/usr/sbin/nologin\n"

	passwdPath := filepath.Join(tmp, "passwd")
	if err := os.WriteFile(passwdPath, []byte(passwdContent), 0644); err != nil {
		t.Fatal(err)
	}

	shellOK := func(sh string) bool {
		return sh == "/bin/bash" || sh == "/bin/zsh"
	}

	// 1. Non-root only, prioritized by preferredUser=deploy, then UID ascending.
	users := ListSystemUsersFrom(passwdPath, "deploy", isExistingDir, shellOK, false)
	if len(users) != 3 {
		t.Fatalf("expected 3 non-root users, got %d: %+v", len(users), users)
	}
	if users[0].Username != "deploy" || users[1].Username != "alice" || users[2].Username != "bob" {
		t.Fatalf("unexpected non-root user order: %+v", users)
	}
	for _, u := range users {
		if u.IsRoot {
			t.Fatalf("did not expect root user when includeRoot=false: %+v", u)
		}
	}

	// 2. Include root places root at the end.
	withRoot := ListSystemUsersFrom(passwdPath, "", isExistingDir, shellOK, true)
	if len(withRoot) != 4 {
		t.Fatalf("expected 4 users with root, got %d: %+v", len(withRoot), withRoot)
	}
	if withRoot[0].Username != "alice" || !withRoot[3].IsRoot || withRoot[3].Username != "root" {
		t.Fatalf("unexpected ordering with root: %+v", withRoot)
	}
}
