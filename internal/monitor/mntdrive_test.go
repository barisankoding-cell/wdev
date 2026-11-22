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

func TestDetectOtherMountsFiltersSystemAndIncludesExternal(t *testing.T) {
	tmp := t.TempDir()
	usbDir := filepath.Join(tmp, "usb-backup")
	rcloneDir := filepath.Join(tmp, "cloud-drive")
	dataDir := filepath.Join(tmp, "data")

	for _, dir := range []string{usbDir, rcloneDir, dataDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	mountsFile := filepath.Join(tmp, "mounts")
	mountsContent := strings.Join([]string{
		"/dev/nvme0n1p6 / ext4 rw,relatime 0 0",
		"/dev/nvme0n1p6 /home ext4 rw,relatime 0 0",
		"/dev/nvme0n1p4 /boot ext4 rw,relatime 0 0",
		"/dev/nvme0n1p5 /boot/efi vfat rw 0 0",
		"tmpfs /tmp tmpfs rw 0 0",
		"/dev/loop0 /snap/core/123 squashfs ro 0 0",
		fmt.Sprintf("/dev/sdb1 %s exfat rw 0 0", usbDir),
		fmt.Sprintf("remote: %s fuse.rclone rw 0 0", rcloneDir),
		fmt.Sprintf("/dev/sdc1 %s ext4 rw 0 0", dataDir),
		fmt.Sprintf("/dev/sdc1 %s/bind ext4 rw 0 0", dataDir),
	}, "\n") + "\n"

	if err := os.WriteFile(mountsFile, []byte(mountsContent), 0644); err != nil {
		t.Fatal(err)
	}

	fakeStatfs := func(path string, buf *syscall.Statfs_t) error {
		const blocksPerGiB = (1024 * 1024 * 1024) / 4096
		buf.Bsize = 4096
		switch path {
		case usbDir:
			buf.Blocks = 200 * blocksPerGiB
			buf.Bfree = 50 * blocksPerGiB
			return nil
		case rcloneDir:
			buf.Blocks = 1000 * blocksPerGiB
			buf.Bfree = 900 * blocksPerGiB
			return nil
		case dataDir:
			buf.Blocks = 500 * blocksPerGiB
			buf.Bfree = 250 * blocksPerGiB
			return nil
		default:
			return syscall.ENOENT
		}
	}

	got := detectOtherMounts(mountsFile, fakeStatfs, nil)
	if len(got) != 3 {
		t.Fatalf("expected 3 external/other mounts, got %d (%+v)", len(got), got)
	}
	byMP := map[string]MountDriveInfo{}
	for _, d := range got {
		byMP[d.MountPoint] = d
	}
	if d, ok := byMP[usbDir]; !ok || d.PercentExact != 75 || d.FSType != "exfat" {
		t.Fatalf("unexpected usb mount: %+v", d)
	}
	if d, ok := byMP[rcloneDir]; !ok || d.PercentExact != 10 || d.FSType != "fuse.rclone" {
		t.Fatalf("unexpected rclone mount: %+v", d)
	}
	if d, ok := byMP[dataDir]; !ok || d.PercentExact != 50 {
		t.Fatalf("unexpected data mount: %+v", d)
	}
}
