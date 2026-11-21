package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDetectMountDrives(t *testing.T) {
	tmp := t.TempDir()
	mntBase := filepath.Join(tmp, "mnt")
	dataDir := filepath.Join(mntBase, "data")
	emptyDir := filepath.Join(mntBase, "unused")
	autoDir := filepath.Join(mntBase, "autoonly")

	for _, dir := range []string{mntBase, dataDir, emptyDir, autoDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	mountsFile := filepath.Join(tmp, "mounts")
	mountsContent := strings.Join([]string{
		"/dev/sda1 / ext4 rw,relatime 0 0",
		fmt.Sprintf("systemd-1 %s autofs rw 0 0", autoDir),
		fmt.Sprintf("/dev/sdb1 %s ext4 rw,relatime 0 0", dataDir),
		fmt.Sprintf("/dev/sdc1 %s xfs rw 0 0", filepath.Join(mntBase, "missing")),
	}, "\n") + "\n"

	if err := os.WriteFile(mountsFile, []byte(mountsContent), 0644); err != nil {
		t.Fatal(err)
	}

	fakeStatfs := func(path string, buf *syscall.Statfs_t) error {
		if path != dataDir {
			return syscall.ENOENT
		}
		buf.Bsize = 4096
		// 100 GiB total, 40 GiB free -> 60 GiB used (60%)
		const blocksPerGiB = (1024 * 1024 * 1024) / 4096
		buf.Blocks = 100 * blocksPerGiB
		buf.Bfree = 40 * blocksPerGiB
		return nil
	}

	drives := detectMountDrives(mntBase, mountsFile, fakeStatfs)
	if len(drives) != 1 {
		t.Fatalf("expected 1 detected drive under %s, got %d (%+v)", mntBase, len(drives), drives)
	}

	got := drives[0]
	if got.MountPoint != dataDir {
		t.Fatalf("MountPoint = %q, want %q", got.MountPoint, dataDir)
	}
	if got.Name != fmt.Sprintf("Drive data (%s)", dataDir) {
		t.Fatalf("Name = %q, want %q", got.Name, fmt.Sprintf("Drive data (%s)", dataDir))
	}
	if got.Device != "/dev/sdb1" || got.FSType != "ext4" {
		t.Fatalf("unexpected device/fstype: %+v", got)
	}
	if got.TotalGB != 100 || got.UsedGB != 60 || got.FreeGB != 40 || got.Percent != 60 {
		t.Fatalf("unexpected capacity metrics: %+v", got)
	}
}

func TestDetectMountDrivesEmptyReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	mntBase := filepath.Join(tmp, "mnt")
	if err := os.MkdirAll(mntBase, 0755); err != nil {
		t.Fatal(err)
	}
	mountsFile := filepath.Join(tmp, "mounts")
	if err := os.WriteFile(mountsFile, []byte("/dev/sda1 / ext4 rw 0 0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	drives := detectMountDrives(mntBase, mountsFile, syscall.Statfs)
	if len(drives) != 0 {
		t.Fatalf("expected no drives when /mnt is unmounted, got %+v", drives)
	}
}
