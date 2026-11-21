package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskHistoryRunningAndFinishedScan(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	runningID, err := db.CreateTask("Install NetBird")
	if err != nil {
		t.Fatalf("CreateTask(running) failed: %v", err)
	}

	finishedID, err := db.CreateTask("Check Updates")
	if err != nil {
		t.Fatalf("CreateTask(finished) failed: %v", err)
	}
	if err := db.FinishTask(finishedID, "success", "line-a\nline-b\n"); err != nil {
		t.Fatalf("FinishTask failed: %v", err)
	}

	tasks, err := db.ListRecentTasks(10)
	if err != nil {
		t.Fatalf("ListRecentTasks failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	if tasks[0].ID != finishedID || tasks[0].Status != "success" || tasks[0].LogOutput != "line-a\nline-b\n" {
		t.Fatalf("unexpected finished task: %+v", tasks[0])
	}
	if tasks[0].FinishedAt.IsZero() {
		t.Fatalf("expected non-zero FinishedAt for finished task")
	}

	if tasks[1].ID != runningID || tasks[1].Status != "running" {
		t.Fatalf("unexpected running task: %+v", tasks[1])
	}
	if !tasks[1].FinishedAt.Equal(tasks[1].StartedAt) {
		t.Fatalf("expected running task FinishedAt fallback to StartedAt, got started=%v finished=%v", tasks[1].StartedAt, tasks[1].FinishedAt)
	}
}

func TestSyncFromDiskDetectsStaticAndIgnoresWWWAlias(t *testing.T) {
	tmp := t.TempDir()
	db, err := New(filepath.Join(tmp, "sites.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	avail := filepath.Join(tmp, "sites-available")
	if err := os.MkdirAll(avail, 0755); err != nil {
		t.Fatal(err)
	}
	vhost := `server {
    listen 80;
    server_name portal.my.id www.portal.my.id;
    root /var/www/portal.my.id;
    index index.html index.htm;
    location / {
        try_files $uri $uri/ =404;
    }
}
`
	if err := os.WriteFile(filepath.Join(avail, "portal.my.id"), []byte(vhost), 0644); err != nil {
		t.Fatal(err)
	}

	db.SyncFromDisk(avail, filepath.Join(tmp, "exposures"))

	sites, err := db.ListSites()
	if err != nil {
		t.Fatalf("ListSites failed: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
	s := sites[0]
	if s.Domain != "portal.my.id" || s.Framework != "static" || s.PHPVersion != "" || s.RootPath != "/var/www/portal.my.id" {
		t.Fatalf("unexpected site record from SyncFromDisk: %+v", s)
	}
	if s.ExposedTo != "" {
		t.Fatalf("www.portal.my.id should not be treated as exposed_to, got %q", s.ExposedTo)
	}
}

func TestSetSiteSSLAndSyncFromDiskDetectsLetsEncrypt(t *testing.T) {
	tmp := t.TempDir()
	db, err := New(filepath.Join(tmp, "ssl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.SaveSite(SiteRecord{
		Domain:    "nikaget.my.id",
		RootPath:  "/var/www/nikaget.my.id",
		Framework: "static",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSiteSSL("nikaget.my.id", true, "letsencrypt"); err != nil {
		t.Fatal(err)
	}
	sites, err := db.ListSites()
	if err != nil || len(sites) != 1 {
		t.Fatalf("unexpected ListSites: %+v err=%v", sites, err)
	}
	if !sites[0].SSLEnabled || sites[0].SSLProvider != "letsencrypt" {
		t.Fatalf("expected letsencrypt SSL state, got %+v", sites[0])
	}

	avail := filepath.Join(tmp, "sites-available")
	if err := os.MkdirAll(avail, 0755); err != nil {
		t.Fatal(err)
	}
	leConf := `server {
    listen 443 ssl;
    server_name nikaget.my.id www.nikaget.my.id;
    ssl_certificate /etc/letsencrypt/live/nikaget.my.id/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nikaget.my.id/privkey.pem;
    root /var/www/nikaget.my.id;
}
`
	if err := os.WriteFile(filepath.Join(avail, "nikaget.my.id"), []byte(leConf), 0644); err != nil {
		t.Fatal(err)
	}
	db.SyncFromDisk(avail, filepath.Join(tmp, "exposures"))

	sites, err = db.ListSites()
	if err != nil || len(sites) != 1 {
		t.Fatalf("unexpected ListSites after sync: %+v err=%v", sites, err)
	}
	if !sites[0].SSLEnabled || sites[0].SSLProvider != "letsencrypt" {
		t.Fatalf("SyncFromDisk did not detect letsencrypt provider: %+v", sites[0])
	}
}
